package consensus

import (
        "crypto/ecdsa"
        "encoding/json"
        "fmt"
        "log"
        "math/big"
        "sync"
        "time"

        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
)

type ValidatorStatus string

const (
        StatusPending    ValidatorStatus = "pending"
        StatusActive     ValidatorStatus = "active"
        StatusInactive   ValidatorStatus = "inactive"
        StatusSlashed    ValidatorStatus = "slashed"
        StatusExiting    ValidatorStatus = "exiting"
)

type ValidatorRegistry struct {
        state               *blockchain.State
        validators          map[string]*ValidatorRegistration
        mu                  sync.RWMutex
        activationDelay     time.Duration
        exitDelay           time.Duration
}

type ValidatorRegistration struct {
        ValidatorID       string
        PublicKey         []byte
        Status            ValidatorStatus
        RegistrationTime  time.Time
        ObserverStartTime time.Time
        ActivationTime    time.Time
        ExitTime          time.Time
        RewardAddress     string
}

type RegistrationTx struct {
        ValidatorID   string `json:"validator_id"`
        PublicKey     []byte `json:"public_key"`
        RewardAddress string `json:"reward_address"`
        Signature     []byte `json:"signature"`
}

func NewValidatorRegistry(state *blockchain.State) *ValidatorRegistry {
        return &ValidatorRegistry{
                state:            state,
                validators:       make(map[string]*ValidatorRegistration),
                activationDelay:  10 * time.Second,
                exitDelay:        200 * time.Second,
        }
}

func calculateObserverDuration(activeValidatorCount int) time.Duration {
        const (
                minValidators = 100
                maxValidators = 1000
                minDuration   = 6.0  // hours
                maxDuration   = 24.0 // hours
        )

        if activeValidatorCount < minValidators {
                return time.Duration(minDuration * float64(time.Hour))
        } else if activeValidatorCount >= maxValidators {
                return time.Duration(maxDuration * float64(time.Hour))
        } else {
                progress := float64(activeValidatorCount-minValidators) / float64(maxValidators-minValidators)
                durationHours := minDuration + progress*(maxDuration-minDuration)
                return time.Duration(durationHours * float64(time.Hour))
        }
}

// calculateDynamicEntryFee implements Whitepaper Bab 5.1.2
// "Biaya masuk untuk menyediakan kapasitas bandwidth setara dengan durasi operasional validator"
// Entry fee = total MB-hours (TIDAK ADA konversi RNR, langsung MB-hours)
func calculateDynamicEntryFee(activeValidatorCount int, observerDuration time.Duration) *big.Int {
        // Whitepaper Bab 5.1.2: Entry fee hanya berdasarkan MB-hours
        // Phases: <100 validators = 6h, 100-1000 = linear, >1000 = 24h
        
        const (
                bandwidthMBps = 7.0  // MB/s minimum requirement dari whitepaper
        )
        
        durationHours := observerDuration.Hours()
        
        // Total MB yang harus disediakan = bandwidth (MB/s) × duration (seconds)
        totalMBTransmitted := bandwidthMBps * durationHours * 3600 // MB/s × hours × 3600s/h = total MB
        
        // Entry fee = total MB-hours (tanpa konversi RNR!)
        // Scale dengan precision 8 decimal untuk compatibility dengan big.Int
        entryFeeMBHours := totalMBTransmitted * 1e8 // Scale for precision
        entryFee := big.NewInt(int64(entryFeeMBHours))
        
        log.Printf("💰 Dynamic Entry Fee: %d validators → %v duration → %.2f MB-hours", 
                activeValidatorCount, observerDuration, totalMBTransmitted)
        
        return entryFee
}

func (vr *ValidatorRegistry) RegisterValidator(tx *RegistrationTx) error {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        if _, exists := vr.validators[tx.ValidatorID]; exists {
                return fmt.Errorf("validator already registered: %s", tx.ValidatorID)
        }

        now := time.Now()
        activeValidatorCount := len(vr.state.GetActiveValidators())
        observerDuration := calculateObserverDuration(activeValidatorCount)
        
        // Whitepaper Bab 5.1.2: Charge dynamic entry fee (dalam MB-hours, bukan RNR)
        entryFeeMBHours := calculateDynamicEntryFee(activeValidatorCount, observerDuration)
        account, err := vr.state.GetAccount(tx.ValidatorID)
        if err == nil && account != nil {
                if account.Balance.Cmp(entryFeeMBHours) < 0 {
                        return fmt.Errorf("insufficient balance for entry fee: required %s MB-hours, has %s balance", 
                                entryFeeMBHours.String(), account.Balance.String())
                }
                // Deduct entry fee (burned untuk prevent Sybil attacks - Whitepaper Bab 5.1.2)
                account.Balance = new(big.Int).Sub(account.Balance, entryFeeMBHours)
                vr.state.UpdateAccount(account)
                log.Printf("🔥 Entry fee burned: %s MB-hours from %s", entryFeeMBHours.String(), tx.ValidatorID[:12])
        }
        
        registration := &ValidatorRegistration{
                ValidatorID:       tx.ValidatorID,
                PublicKey:         tx.PublicKey,
                Status:            StatusPending,
                RegistrationTime:  now,
                ObserverStartTime: now,
                RewardAddress:     tx.RewardAddress,
        }

        vr.validators[tx.ValidatorID] = registration

        validatorInfo := &core.ValidatorInfo{
                ID:                tx.ValidatorID,
                PublicKey:         tx.PublicKey,
                PoBScore:          0.0,
                Reputation:        100,
                LastPoBTest:       time.Time{},
                IsActive:          false,
                RewardAddress:     tx.RewardAddress,
                NetworkASN:        "unknown",
                IPAddress:         "",
                IsObserver:        true,
                ObserverStartTime: now,
                ObserverDuration:  observerDuration,
        }
        vr.state.UpdateValidator(validatorInfo)

        shortID := tx.ValidatorID
        if len(tx.ValidatorID) > 12 {
                shortID = tx.ValidatorID[:12]
        }
        log.Printf("📝 Validator registered as observer: %s, duration: %v (based on %d active validators)", 
                shortID, observerDuration, activeValidatorCount)

        return nil
}

func (vr *ValidatorRegistry) ActivatePendingValidators() {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        now := time.Now()

        for id, registration := range vr.validators {
                if registration.Status == StatusPending {
                        validatorInfo, err := vr.state.GetValidator(id)
                        if err != nil {
                                continue
                        }

                        if validatorInfo.IsObserver && now.Sub(validatorInfo.ObserverStartTime) >= validatorInfo.ObserverDuration {
                                if validatorInfo.PoBScore >= core.MinPoBScore {
                                        registration.Status = StatusActive
                                        registration.ActivationTime = now

                                        validatorInfo.IsActive = true
                                        validatorInfo.IsObserver = false
                                        vr.state.UpdateValidator(validatorInfo)

                                        shortID := id
                                        if len(id) > 12 {
                                                shortID = id[:12]
                                        }
                                        log.Printf("✅ Observer promoted to active validator: %s (PoB: %.2f)", shortID, validatorInfo.PoBScore)
                                } else {
                                        shortID := id
                                        if len(id) > 12 {
                                                shortID = id[:12]
                                        }
                                        log.Printf("⚠️  Observer failed PoB test: %s (PoB: %.2f < %.2f required)", shortID, validatorInfo.PoBScore, core.MinPoBScore)
                                }
                        }
                }
        }
}

func (vr *ValidatorRegistry) RequestExit(validatorID string) error {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        registration, exists := vr.validators[validatorID]
        if !exists {
                return fmt.Errorf("validator not found: %s", validatorID)
        }

        if registration.Status != StatusActive {
                return fmt.Errorf("validator not active: %s", validatorID)
        }

        registration.Status = StatusExiting
        registration.ExitTime = time.Now()

        validatorInfo, err := vr.state.GetValidator(validatorID)
        if err == nil {
                validatorInfo.IsActive = false
                vr.state.UpdateValidator(validatorInfo)
        }

        shortID := validatorID
        if len(validatorID) > 12 {
                shortID = validatorID[:12]
        }
        log.Printf("🚪 Validator exit requested: %s", shortID)

        return nil
}

func (vr *ValidatorRegistry) ProcessExits() {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        now := time.Now()

        for id, registration := range vr.validators {
                if registration.Status == StatusExiting &&
                        now.Sub(registration.ExitTime) >= vr.exitDelay {

                        registration.Status = StatusInactive

                        delete(vr.validators, id)

                        shortID := id
                        if len(id) > 12 {
                                shortID = id[:12]
                        }
                        log.Printf("👋 Validator exited: %s", shortID)
                }
        }
}

func (vr *ValidatorRegistry) SuspendValidator(validatorID string, suspensionEndTime time.Time, reason string) error {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        validatorInfo, err := vr.state.GetValidator(validatorID)
        if err != nil {
                return fmt.Errorf("validator not found: %w", err)
        }

        validatorInfo.IsSuspended = true
        validatorInfo.SuspensionEndTime = suspensionEndTime
        validatorInfo.SuspensionReason = reason

        vr.state.UpdateValidator(validatorInfo)

        shortID := validatorID
        if len(validatorID) > 12 {
                shortID = validatorID[:12]
        }
        log.Printf("⚡ Validator suspended (observer mode): %s, reason: %s, until: %s",
                shortID, reason, suspensionEndTime.Format("15:04:05"))

        return nil
}

func (vr *ValidatorRegistry) ClearSuspension(validatorID string) error {
        vr.mu.Lock()
        defer vr.mu.Unlock()

        validatorInfo, err := vr.state.GetValidator(validatorID)
        if err != nil {
                return fmt.Errorf("validator not found: %w", err)
        }

        validatorInfo.IsSuspended = false
        validatorInfo.SuspensionEndTime = time.Time{}
        validatorInfo.SuspensionReason = ""

        vr.state.UpdateValidator(validatorInfo)

        return nil
}

func (vr *ValidatorRegistry) GetAllValidators() []*core.ValidatorInfo {
        vr.mu.RLock()
        defer vr.mu.RUnlock()

        validators := make([]*core.ValidatorInfo, 0)
        
        for id := range vr.validators {
                if validatorInfo, err := vr.state.GetValidator(id); err == nil {
                        validators = append(validators, validatorInfo)
                }
        }

        return validators
}

func (vr *ValidatorRegistry) GetActiveValidators() []string {
        vr.mu.RLock()
        defer vr.mu.RUnlock()

        active := make([]string, 0)
        for id, registration := range vr.validators {
                if registration.Status == StatusActive {
                        active = append(active, id)
                }
        }

        return active
}

func (vr *ValidatorRegistry) GetValidatorInfo(validatorID string) (*ValidatorRegistration, error) {
        vr.mu.RLock()
        defer vr.mu.RUnlock()

        registration, exists := vr.validators[validatorID]
        if !exists {
                return nil, fmt.Errorf("validator not found: %s", validatorID)
        }

        return registration, nil
}

func (vr *ValidatorRegistry) GetActiveValidatorCount() int {
        vr.mu.RLock()
        defer vr.mu.RUnlock()

        count := 0
        for _, registration := range vr.validators {
                if registration.Status == StatusActive {
                        count++
                }
        }

        return count
}

func (tx *RegistrationTx) Marshal() ([]byte, error) {
        return json.Marshal(tx)
}

func UnmarshalRegistrationTx(data []byte) (*RegistrationTx, error) {
        var tx RegistrationTx
        err := json.Unmarshal(data, &tx)
        return &tx, err
}

func (tx *RegistrationTx) Sign(privateKey *ecdsa.PrivateKey) error {
        data, err := json.Marshal(struct {
                ValidatorID   string
                PublicKey     []byte
                RewardAddress string
        }{
                ValidatorID:   tx.ValidatorID,
                PublicKey:     tx.PublicKey,
                RewardAddress: tx.RewardAddress,
        })
        if err != nil {
                return err
        }

        signature, err := SignVote(data, privateKey)
        if err != nil {
                return err
        }

        tx.Signature = signature
        return nil
}
