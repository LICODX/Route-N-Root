package consensus

import (
        "encoding/json"
        "fmt"
        "log"
        "sync"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
)

type SlashingReason string

const (
        ReasonDoubleVoting   SlashingReason = "double_voting"
        ReasonDowntime       SlashingReason = "downtime"
        ReasonInvalidBlock   SlashingReason = "invalid_block"
        ReasonInvalidVote    SlashingReason = "invalid_vote"
        ReasonMaliciousBehavior SlashingReason = "malicious_behavior"
)

type SlashingEvent struct {
        ValidatorID        string         `json:"validator_id"`
        Reason             SlashingReason `json:"reason"`
        SuspensionDuration time.Duration  `json:"suspension_duration"`
        SuspensionEndTime  time.Time      `json:"suspension_end_time"`
        Evidence           []byte         `json:"evidence"`
        Timestamp          time.Time      `json:"timestamp"`
        BlockHeight        uint64         `json:"block_height"`
}

type SlashingManager struct {
        db                *leveldb.DB
        registry          *ValidatorRegistry
        events            []*SlashingEvent
        doubleVoteTracker map[string]map[uint64][]byte
        mu                sync.RWMutex
        suspensionDurations map[SlashingReason]time.Duration
}

func NewSlashingManager(db *leveldb.DB, registry *ValidatorRegistry) *SlashingManager {
        sm := &SlashingManager{
                db:                db,
                registry:          registry,
                events:            make([]*SlashingEvent, 0),
                doubleVoteTracker: make(map[string]map[uint64][]byte),
                suspensionDurations: make(map[SlashingReason]time.Duration),
        }

        sm.initializeSuspensionDurations()
        sm.loadSlashingEvents()

        return sm
}

func (sm *SlashingManager) initializeSuspensionDurations() {
        sm.suspensionDurations[ReasonDoubleVoting] = 24 * time.Hour
        sm.suspensionDurations[ReasonDowntime] = 6 * time.Hour
        sm.suspensionDurations[ReasonInvalidBlock] = 24 * time.Hour
        sm.suspensionDurations[ReasonInvalidVote] = 1 * time.Hour
        sm.suspensionDurations[ReasonMaliciousBehavior] = 24 * time.Hour
}

func (sm *SlashingManager) loadSlashingEvents() {
        iter := sm.db.NewIterator(nil, nil)
        defer iter.Release()

        for iter.Next() {
                key := string(iter.Key())
                if len(key) > 8 && key[:8] == "slashing_" {
                        var event SlashingEvent
                        if err := json.Unmarshal(iter.Value(), &event); err == nil {
                                sm.events = append(sm.events, &event)
                        }
                }
        }

        log.Printf("⚡ Loaded %d slashing events", len(sm.events))
}

func (sm *SlashingManager) DetectDoubleVoting(validatorID string, blockHeight uint64, blockHash []byte) error {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        if _, exists := sm.doubleVoteTracker[validatorID]; !exists {
                sm.doubleVoteTracker[validatorID] = make(map[uint64][]byte)
        }

        previousVote, exists := sm.doubleVoteTracker[validatorID][blockHeight]
        if exists {
                if string(previousVote) != string(blockHash) {
                        evidence := append(previousVote, blockHash...)
                        return sm.slashValidator(validatorID, ReasonDoubleVoting, evidence, blockHeight)
                }
        } else {
                sm.doubleVoteTracker[validatorID][blockHeight] = blockHash
        }

        return nil
}

func (sm *SlashingManager) CheckDowntime(validatorID string, lastSeen time.Time, blockHeight uint64) error {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        downtimeThreshold := 1 * time.Hour

        if time.Since(lastSeen) > downtimeThreshold {
                evidence := []byte(fmt.Sprintf("Last seen: %s", lastSeen.String()))
                return sm.slashValidator(validatorID, ReasonDowntime, evidence, blockHeight)
        }

        return nil
}

func (sm *SlashingManager) SlashForInvalidBlock(validatorID string, evidence []byte, blockHeight uint64) error {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        return sm.slashValidator(validatorID, ReasonInvalidBlock, evidence, blockHeight)
}

func (sm *SlashingManager) slashValidator(validatorID string, reason SlashingReason, evidence []byte, blockHeight uint64) error {
        duration, exists := sm.suspensionDurations[reason]
        if !exists {
                duration = 1 * time.Hour
        }

        suspensionEndTime := time.Now().Add(duration)

        if err := sm.registry.SuspendValidator(validatorID, suspensionEndTime, string(reason)); err != nil {
                return fmt.Errorf("failed to suspend validator: %w", err)
        }

        event := &SlashingEvent{
                ValidatorID:        validatorID,
                Reason:             reason,
                SuspensionDuration: duration,
                SuspensionEndTime:  suspensionEndTime,
                Evidence:           evidence,
                Timestamp:          time.Now(),
                BlockHeight:        blockHeight,
        }

        sm.events = append(sm.events, event)

        eventBytes, err := json.Marshal(event)
        if err != nil {
                return err
        }

        key := fmt.Sprintf("slashing_%s_%d", validatorID, blockHeight)
        if err := sm.db.Put([]byte(key), eventBytes, nil); err != nil {
                return err
        }

        shortID := validatorID
        if len(validatorID) > 12 {
                shortID = validatorID[:12]
        }
        log.Printf("⚡ Validator suspended: %s, reason: %s, duration: %v (until %s)",
                shortID, reason, duration, suspensionEndTime.Format("15:04:05"))

        return nil
}

func (sm *SlashingManager) GetSlashingHistory(validatorID string) []*SlashingEvent {
        sm.mu.RLock()
        defer sm.mu.RUnlock()

        history := make([]*SlashingEvent, 0)
        for _, event := range sm.events {
                if event.ValidatorID == validatorID {
                        history = append(history, event)
                }
        }

        return history
}

func (sm *SlashingManager) CheckAndClearExpiredSuspensions() int {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        now := time.Now()
        cleared := 0

        validators := sm.registry.GetAllValidators()
        for _, validator := range validators {
                if validator.IsSuspended && now.After(validator.SuspensionEndTime) {
                        if err := sm.registry.ClearSuspension(validator.ID); err == nil {
                                shortID := validator.ID
                                if len(validator.ID) > 12 {
                                        shortID = validator.ID[:12]
                                }
                                log.Printf("✅ Suspension cleared for validator: %s (observer mode ended)", shortID)
                                cleared++
                        }
                }
        }

        return cleared
}

func (sm *SlashingManager) CleanupOldTracking(maxAge time.Duration) {
        sm.mu.Lock()
        defer sm.mu.Unlock()

        cutoff := time.Now().Add(-maxAge)

        newEvents := make([]*SlashingEvent, 0)
        for _, event := range sm.events {
                if event.Timestamp.After(cutoff) {
                        newEvents = append(newEvents, event)
                }
        }

        sm.events = newEvents

        log.Printf("🧹 Cleaned up old slashing events: %d remaining", len(sm.events))
}
