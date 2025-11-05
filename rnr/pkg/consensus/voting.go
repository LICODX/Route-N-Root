package consensus

import (
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "errors"
        "fmt"
        "log"
        "math/big"
        "sync"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
)

type Vote struct {
        BlockHash   []byte
        ValidatorID string
        Signature   []byte
        Timestamp   time.Time
        VoteID      string  // SECURITY: Unique vote ID for replay protection
}

type VotingSession struct {
        BlockHash          []byte
        Votes              map[string]*Vote
        TotalValidators    int
        RequiredVotes      int
        StartTime          time.Time
        VotingDeadline     time.Time
        IsFinalized        bool
        FinalityAchievedAt time.Time
        mu                 sync.RWMutex
}

type VotingManager struct {
        sessions     map[string]*VotingSession
        state        *blockchain.State
        db           *leveldb.DB
        processedVotes map[string]bool  // SECURITY: Vote replay protection - track processed vote IDs
        mu           sync.RWMutex
}

func NewVotingManager(db *leveldb.DB) *VotingManager {
        vm := &VotingManager{
                sessions:       make(map[string]*VotingSession),
                db:             db,
                processedVotes: make(map[string]bool),  // Track vote IDs to prevent replay
        }
        
        // Load processed votes from LevelDB
        if db != nil {
                if err := vm.LoadProcessedVotes(); err != nil {
                        log.Printf("⚠️  Failed to load processed votes: %v", err)
                }
        }
        
        return vm
}

func (vm *VotingManager) SetState(state *blockchain.State) {
        vm.state = state
}

func (vm *VotingManager) StartVotingSession(blockHash []byte, totalValidators int) (*VotingSession, error) {
        vm.mu.Lock()
        defer vm.mu.Unlock()

        blockHashStr := hex.EncodeToString(blockHash)
        
        if _, exists := vm.sessions[blockHashStr]; exists {
                return nil, fmt.Errorf("voting session already exists for block %s", blockHashStr[:8])
        }

        requiredVotes := int(float64(totalValidators) * core.SupermajorityThreshold)
        if requiredVotes == 0 {
                requiredVotes = 1
        }

        session := &VotingSession{
                BlockHash:       blockHash,
                Votes:           make(map[string]*Vote),
                TotalValidators: totalValidators,
                RequiredVotes:   requiredVotes,
                StartTime:       time.Now(),
                VotingDeadline:  time.Now().Add(core.VerificationVotingPhase),
                IsFinalized:     false,
        }

        vm.sessions[blockHashStr] = session
        return session, nil
}

func (vm *VotingManager) CastVote(blockHash []byte, validatorID string, signature []byte) error {
        vm.mu.Lock()
        defer vm.mu.Unlock()

        blockHashStr := hex.EncodeToString(blockHash)
        session, exists := vm.sessions[blockHashStr]
        if !exists {
                return fmt.Errorf("no voting session found for block %s", blockHashStr[:8])
        }

        session.mu.Lock()
        defer session.mu.Unlock()

        if session.IsFinalized {
                return errors.New("voting session already finalized")
        }

        if time.Now().After(session.VotingDeadline) {
                return errors.New("voting deadline exceeded")
        }

        // SECURITY FIX: Check if validator already voted in this session
        if _, hasVoted := session.Votes[validatorID]; hasVoted {
                return errors.New("validator has already voted in this session")
        }

        // SECURITY FIX: Generate deterministic vote ID (blockHash + validatorID only)
        // DO NOT include signature - it's nondeterministic and allows replay via re-signing
        voteID := generateVoteID(blockHash, validatorID)
        if vm.processedVotes[voteID] {
                return fmt.Errorf("replay attack detected: vote %s already processed", voteID[:8])
        }
        
        // Create vote object for persistence
        vote := &Vote{
                BlockHash:   blockHash,
                ValidatorID: validatorID,
                Signature:   signature,
                Timestamp:   time.Now(),
                VoteID:      voteID,
        }

        if vm.state != nil {
                activeValidators := vm.state.GetActiveValidators()
                isActiveValidator := false
                for _, vid := range activeValidators {
                        if vid == validatorID {
                                isActiveValidator = true
                                break
                        }
                }
                if !isActiveValidator {
                        shortID := validatorID
                        if len(validatorID) > 12 {
                                shortID = validatorID[:12]
                        }
                        return fmt.Errorf("validator %s is not in active validator set", shortID)
                }

                validatorInfo, err := vm.state.GetValidator(validatorID)
                if err != nil {
                        return fmt.Errorf("failed to get validator info: %w", err)
                }

                if validatorInfo.IsSuspended {
                        shortID := validatorID
                        if len(validatorID) > 12 {
                                shortID = validatorID[:12]
                        }
                        return fmt.Errorf("validator %s is suspended (observer mode) - cannot vote", shortID)
                }

                publicKey, err := DecodeECDSAPublicKey(validatorInfo.PublicKey)
                if err != nil {
                        return fmt.Errorf("failed to decode validator public key: %w", err)
                }

                if !VerifyVote(vote, publicKey) {
                        shortID := validatorID
                        if len(validatorID) > 12 {
                                shortID = validatorID[:12]
                        }
                        return fmt.Errorf("invalid vote signature from validator %s", shortID)
                }
        }

        session.Votes[validatorID] = vote
        
        // Persist vote to LevelDB
        if err := vm.PersistVote(vote); err != nil {
                log.Printf("⚠️  Failed to persist vote: %v", err)
        }
        
        // SECURITY FIX: Mark vote as processed to prevent replay attacks
        vm.processedVotes[voteID] = true

        if len(session.Votes) >= session.RequiredVotes && !session.IsFinalized {
                session.IsFinalized = true
                session.FinalityAchievedAt = time.Now()
        }

        return nil
}

func (vm *VotingManager) CheckFinality(blockHash []byte) (bool, int, error) {
        vm.mu.RLock()
        defer vm.mu.RUnlock()

        blockHashStr := hex.EncodeToString(blockHash)
        session, exists := vm.sessions[blockHashStr]
        if !exists {
                return false, 0, fmt.Errorf("no voting session found")
        }

        session.mu.RLock()
        defer session.mu.RUnlock()

        return session.IsFinalized, len(session.Votes), nil
}

func (vm *VotingManager) GetVotes(blockHash []byte) ([]*Vote, error) {
        vm.mu.RLock()
        defer vm.mu.RUnlock()

        blockHashStr := hex.EncodeToString(blockHash)
        session, exists := vm.sessions[blockHashStr]
        if !exists {
                return nil, fmt.Errorf("no voting session found")
        }

        session.mu.RLock()
        defer session.mu.RUnlock()

        votes := make([]*Vote, 0, len(session.Votes))
        for _, vote := range session.Votes {
                votes = append(votes, vote)
        }

        return votes, nil
}

func (vm *VotingManager) CleanupOldSessions(maxAge time.Duration) {
        vm.mu.Lock()
        defer vm.mu.Unlock()

        now := time.Now()
        for hash, session := range vm.sessions {
                session.mu.RLock()
                age := now.Sub(session.StartTime)
                session.mu.RUnlock()

                if age > maxAge {
                        delete(vm.sessions, hash)
                }
        }
}

func SignVote(blockHash []byte, privateKey *ecdsa.PrivateKey) ([]byte, error) {
        r, s, err := ecdsa.Sign(rand.Reader, privateKey, blockHash)
        if err != nil {
                return nil, fmt.Errorf("failed to sign vote: %w", err)
        }
        signature := append(r.Bytes(), s.Bytes()...)
        return signature, nil
}

func VerifyVote(vote *Vote, publicKey *ecdsa.PublicKey) bool {
        if len(vote.Signature) < 32 {
                return false
        }

        r := new(big.Int).SetBytes(vote.Signature[:len(vote.Signature)/2])
        s := new(big.Int).SetBytes(vote.Signature[len(vote.Signature)/2:])

        return ecdsa.Verify(publicKey, vote.BlockHash, r, s)
}

func DecodeECDSAPublicKey(pubKeyBytes []byte) (*ecdsa.PublicKey, error) {
        if len(pubKeyBytes) == 0 {
                return nil, fmt.Errorf("empty public key")
        }

        // New format: 64 bytes (32 for X, 32 for Y)
        if len(pubKeyBytes) == 64 {
                x := new(big.Int).SetBytes(pubKeyBytes[:32])
                y := new(big.Int).SetBytes(pubKeyBytes[32:])
                
                return &ecdsa.PublicKey{
                        Curve: elliptic.P256(),
                        X:     x,
                        Y:     y,
                }, nil
        }

        // Legacy format: hanya X coordinate (untuk backward compatibility)
        x := new(big.Int).SetBytes(pubKeyBytes)
        y := new(big.Int)

        curve := elliptic.P256()
        ySquared := new(big.Int).Mul(x, x)
        ySquared.Mul(ySquared, x)
        threeX := new(big.Int).Mul(x, big.NewInt(3))
        ySquared.Sub(ySquared, threeX)
        ySquared.Add(ySquared, curve.Params().B)
        ySquared.Mod(ySquared, curve.Params().P)

        y.ModSqrt(ySquared, curve.Params().P)

        return &ecdsa.PublicKey{
                Curve: curve,
                X:     x,
                Y:     y,
        }, nil
}

type FinalityTracker struct {
        finalizedBlocks map[string]time.Time
        mu              sync.RWMutex
}

func NewFinalityTracker() *FinalityTracker {
        return &FinalityTracker{
                finalizedBlocks: make(map[string]time.Time),
        }
}

func (ft *FinalityTracker) MarkFinalized(blockHash []byte) {
        ft.mu.Lock()
        defer ft.mu.Unlock()
        ft.finalizedBlocks[hex.EncodeToString(blockHash)] = time.Now()
}

func (ft *FinalityTracker) IsFinalized(blockHash []byte) bool {
        ft.mu.RLock()
        defer ft.mu.RUnlock()
        _, exists := ft.finalizedBlocks[hex.EncodeToString(blockHash)]
        return exists
}

func (ft *FinalityTracker) GetFinalityTime(blockHash []byte) (time.Time, bool) {
        ft.mu.RLock()
        defer ft.mu.RUnlock()
        t, exists := ft.finalizedBlocks[hex.EncodeToString(blockHash)]
        return t, exists
}

func CreateVoteMessage(blockHash []byte, validatorID string) []byte {
        message := append(blockHash, []byte(validatorID)...)
        hash := sha256.Sum256(message)
        return hash[:]
}
