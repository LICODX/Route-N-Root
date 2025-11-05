package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// SECURITY: Byzantine Fault Detection System
// Detects malicious validators attempting to attack the network

type ByzantineEvidence struct {
	ValidatorID   string
	EvidenceType  string // "double_vote", "invalid_vrf", "conflicting_block", "censorship", "speedtest_cheat"
	Timestamp     time.Time
	Proof         []byte  // Cryptographic proof of misbehavior
	Severity      int     // 1-10
	Description   string
	Verified      bool    // Has been verified by committee
}

type ValidatorBehavior struct {
	ValidatorID          string
	TotalBlocks          int
	InvalidBlocks        int
	DoubleVotes          int
	VRFFailures          int
	CensorshipAttempts   int
	SpeedTestCheats      int
	LastMisbehavior      time.Time
	MisbehaviorCount     int
	TrustScore           int // 0-100
	IsUnderInvestigation bool
	IsBanned             bool
	BanUntil             time.Time
}

type ByzantineDetector struct {
	mu                sync.RWMutex
	validators        map[string]*ValidatorBehavior
	evidenceLog       []ByzantineEvidence
	
	// Detection thresholds
	doubleVoteThreshold       int
	vrfFailureThreshold       int
	censorshipThreshold       int
	speedTestCheatThreshold   int
	
	// Trust score configuration
	initialTrustScore         int
	minimumTrustScore         int
	banDuration              time.Duration
}

func NewByzantineDetector() *ByzantineDetector {
	return &ByzantineDetector{
		validators:                make(map[string]*ValidatorBehavior),
		evidenceLog:              make([]ByzantineEvidence, 0),
		doubleVoteThreshold:       3,  // 3 double votes = ban
		vrfFailureThreshold:       5,  // 5 invalid VRFs = ban
		censorshipThreshold:       10, // 10 censorship attempts = ban
		speedTestCheatThreshold:   2,  // 2 speedtest cheats = ban
		initialTrustScore:        100,
		minimumTrustScore:        30,
		banDuration:              7 * 24 * time.Hour, // 7 days
	}
}

// DetectDoubleVote detects if validator votes for multiple blocks at same height
// CRITICAL: This is Byzantine fault - validator trying to fork chain
func (bd *ByzantineDetector) DetectDoubleVote(validatorID string, blockHeight uint64, vote1Hash []byte, vote2Hash []byte) error {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	
	validator := bd.getOrCreateValidator(validatorID)
	
	// Create cryptographic proof
	proof := bd.createDoubleVoteProof(blockHeight, vote1Hash, vote2Hash)
	
	evidence := ByzantineEvidence{
		ValidatorID:  validatorID,
		EvidenceType: "double_vote",
		Timestamp:    time.Now(),
		Proof:        proof,
		Severity:     10, // Maximum severity
		Description:  fmt.Sprintf("Double vote at height %d", blockHeight),
		Verified:     true,
	}
	
	bd.evidenceLog = append(bd.evidenceLog, evidence)
	validator.DoubleVotes++
	validator.MisbehaviorCount++
	validator.LastMisbehavior = time.Now()
	validator.TrustScore -= 30 // Heavy penalty
	
	// Check if should ban
	if validator.DoubleVotes >= bd.doubleVoteThreshold || validator.TrustScore < bd.minimumTrustScore {
		validator.IsBanned = true
		validator.BanUntil = time.Now().Add(bd.banDuration)
		return fmt.Errorf("validator %s BANNED for double voting", validatorID)
	}
	
	return fmt.Errorf("double vote detected for validator %s", validatorID)
}

// DetectInvalidVRF detects VRF manipulation attempts
func (bd *ByzantineDetector) DetectInvalidVRF(validatorID string, vrfInput []byte, claimedOutput []byte, proof []byte) error {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	
	validator := bd.getOrCreateValidator(validatorID)
	
	evidence := ByzantineEvidence{
		ValidatorID:  validatorID,
		EvidenceType: "invalid_vrf",
		Timestamp:    time.Now(),
		Proof:        proof,
		Severity:     9,
		Description:  "VRF verification failed - possible manipulation",
		Verified:     true,
	}
	
	bd.evidenceLog = append(bd.evidenceLog, evidence)
	validator.VRFFailures++
	validator.MisbehaviorCount++
	validator.LastMisbehavior = time.Now()
	validator.TrustScore -= 20
	
	if validator.VRFFailures >= bd.vrfFailureThreshold || validator.TrustScore < bd.minimumTrustScore {
		validator.IsBanned = true
		validator.BanUntil = time.Now().Add(bd.banDuration)
		return fmt.Errorf("validator %s BANNED for VRF manipulation", validatorID)
	}
	
	return fmt.Errorf("invalid VRF detected for validator %s", validatorID)
}

// DetectConflictingBlocks detects if validator proposes multiple blocks at same height
func (bd *ByzantineDetector) DetectConflictingBlocks(validatorID string, height uint64, block1Hash []byte, block2Hash []byte) error {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	
	validator := bd.getOrCreateValidator(validatorID)
	
	proof := bd.createConflictingBlockProof(height, block1Hash, block2Hash)
	
	evidence := ByzantineEvidence{
		ValidatorID:  validatorID,
		EvidenceType: "conflicting_block",
		Timestamp:    time.Now(),
		Proof:        proof,
		Severity:     10,
		Description:  fmt.Sprintf("Conflicting blocks at height %d", height),
		Verified:     true,
	}
	
	bd.evidenceLog = append(bd.evidenceLog, evidence)
	validator.InvalidBlocks++
	validator.MisbehaviorCount++
	validator.LastMisbehavior = time.Now()
	validator.TrustScore -= 35 // Very heavy penalty
	
	// Immediate ban for conflicting blocks
	validator.IsBanned = true
	validator.BanUntil = time.Now().Add(bd.banDuration * 2) // Double duration
	
	return fmt.Errorf("validator %s BANNED for conflicting blocks", validatorID)
}

// DetectSpeedTestCheat detects manipulation in PoB speed tests
func (bd *ByzantineDetector) DetectSpeedTestCheat(validatorID string, cheatType string, evidence []byte) error {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	
	validator := bd.getOrCreateValidator(validatorID)
	
	byzEvidence := ByzantineEvidence{
		ValidatorID:  validatorID,
		EvidenceType: "speedtest_cheat",
		Timestamp:    time.Now(),
		Proof:        evidence,
		Severity:     8,
		Description:  fmt.Sprintf("Speed test cheat: %s", cheatType),
		Verified:     true,
	}
	
	bd.evidenceLog = append(bd.evidenceLog, byzEvidence)
	validator.SpeedTestCheats++
	validator.MisbehaviorCount++
	validator.LastMisbehavior = time.Now()
	validator.TrustScore -= 25
	
	if validator.SpeedTestCheats >= bd.speedTestCheatThreshold || validator.TrustScore < bd.minimumTrustScore {
		validator.IsBanned = true
		validator.BanUntil = time.Now().Add(bd.banDuration)
		return fmt.Errorf("validator %s BANNED for speed test cheating", validatorID)
	}
	
	return fmt.Errorf("speed test cheat detected for validator %s", validatorID)
}

// IsValidatorTrusted checks if validator is trusted
func (bd *ByzantineDetector) IsValidatorTrusted(validatorID string) bool {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	
	validator := bd.validators[validatorID]
	if validator == nil {
		return true // New validators start as trusted
	}
	
	// Check if banned
	if validator.IsBanned {
		if time.Now().Before(validator.BanUntil) {
			return false
		}
		// Ban expired - restore with reduced trust
		validator.IsBanned = false
		validator.TrustScore = bd.minimumTrustScore + 10
	}
	
	return validator.TrustScore >= bd.minimumTrustScore
}

// GetValidatorTrustScore returns trust score for validator
func (bd *ByzantineDetector) GetValidatorTrustScore(validatorID string) int {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	
	validator := bd.validators[validatorID]
	if validator == nil {
		return bd.initialTrustScore
	}
	
	return validator.TrustScore
}

// RecordGoodBehavior increases trust score for good actions
func (bd *ByzantineDetector) RecordGoodBehavior(validatorID string) {
	bd.mu.Lock()
	defer bd.mu.Unlock()
	
	validator := bd.getOrCreateValidator(validatorID)
	validator.TotalBlocks++
	
	// Slow trust recovery
	if validator.TrustScore < bd.initialTrustScore {
		validator.TrustScore += 1
	}
}

// getOrCreateValidator internal helper
func (bd *ByzantineDetector) getOrCreateValidator(validatorID string) *ValidatorBehavior {
	validator, exists := bd.validators[validatorID]
	if !exists {
		validator = &ValidatorBehavior{
			ValidatorID: validatorID,
			TrustScore:  bd.initialTrustScore,
		}
		bd.validators[validatorID] = validator
	}
	return validator
}

// createDoubleVoteProof creates cryptographic proof of double vote
func (bd *ByzantineDetector) createDoubleVoteProof(height uint64, vote1 []byte, vote2 []byte) []byte {
	data := fmt.Sprintf("double_vote_%d_%s_%s", height, hex.EncodeToString(vote1), hex.EncodeToString(vote2))
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

// createConflictingBlockProof creates proof of conflicting blocks
func (bd *ByzantineDetector) createConflictingBlockProof(height uint64, block1 []byte, block2 []byte) []byte {
	data := fmt.Sprintf("conflicting_%d_%s_%s", height, hex.EncodeToString(block1), hex.EncodeToString(block2))
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

// GetByzantineEvidence returns all evidence for a validator
func (bd *ByzantineDetector) GetByzantineEvidence(validatorID string) []ByzantineEvidence {
	bd.mu.RLock()
	defer bd.mu.RUnlock()
	
	evidence := make([]ByzantineEvidence, 0)
	for _, ev := range bd.evidenceLog {
		if ev.ValidatorID == validatorID {
			evidence = append(evidence, ev)
		}
	}
	return evidence
}
