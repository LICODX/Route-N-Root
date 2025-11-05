package consensus

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type AntiDRDoSManager struct {
	activeChallenges  map[string]*Challenge
	testRequestLimits map[string]*RateLimit
	mu                sync.RWMutex
	
	maxRequestsPerHour int
	challengeTTL       time.Duration
	minReputation      float64
}

type Challenge struct {
	ChallengeID   string
	CandidateID   string
	TesterID      string
	Nonce         []byte
	ExpectedHash  string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	Completed     bool
}

type RateLimit struct {
	ValidatorID   string
	RequestCount  int
	WindowStart   time.Time
	LastRequestAt time.Time
}

func NewAntiDRDoSManager() *AntiDRDoSManager {
	return &AntiDRDoSManager{
		activeChallenges:   make(map[string]*Challenge),
		testRequestLimits:  make(map[string]*RateLimit),
		maxRequestsPerHour: 10,
		challengeTTL:       5 * time.Minute,
		minReputation:      0.5,
	}
}

func (ad *AntiDRDoSManager) GenerateChallenge(candidateID, testerID string) (*Challenge, error) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	
	if !ad.checkRateLimit(testerID) {
		return nil, fmt.Errorf("rate limit exceeded for tester %s", testerID)
	}
	
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	
	combinedData := append([]byte(candidateID), nonce...)
	combinedData = append(combinedData, []byte(testerID)...)
	hash := sha256.Sum256(combinedData)
	expectedHash := hex.EncodeToString(hash[:])
	
	challengeID := fmt.Sprintf("%s-%s-%d", candidateID, testerID, time.Now().UnixNano())
	
	challenge := &Challenge{
		ChallengeID:  challengeID,
		CandidateID:  candidateID,
		TesterID:     testerID,
		Nonce:        nonce,
		ExpectedHash: expectedHash,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(ad.challengeTTL),
		Completed:    false,
	}
	
	ad.activeChallenges[challengeID] = challenge
	ad.updateRateLimit(testerID)
	
	return challenge, nil
}

func (ad *AntiDRDoSManager) VerifyChallenge(challengeID string, responseHash string) (bool, error) {
	ad.mu.RLock()
	challenge, exists := ad.activeChallenges[challengeID]
	ad.mu.RUnlock()
	
	if !exists {
		return false, fmt.Errorf("challenge not found: %s", challengeID)
	}
	
	if challenge.Completed {
		return false, fmt.Errorf("challenge already completed")
	}
	
	if time.Now().After(challenge.ExpiresAt) {
		ad.mu.Lock()
		delete(ad.activeChallenges, challengeID)
		ad.mu.Unlock()
		return false, fmt.Errorf("challenge expired")
	}
	
	if challenge.ExpectedHash != responseHash {
		return false, fmt.Errorf("invalid response hash")
	}
	
	ad.mu.Lock()
	challenge.Completed = true
	ad.mu.Unlock()
	
	return true, nil
}

func (ad *AntiDRDoSManager) checkRateLimit(testerID string) bool {
	limit, exists := ad.testRequestLimits[testerID]
	
	if !exists {
		return true
	}
	
	now := time.Now()
	if now.Sub(limit.WindowStart) > time.Hour {
		limit.RequestCount = 0
		limit.WindowStart = now
		return true
	}
	
	if limit.RequestCount >= ad.maxRequestsPerHour {
		return false
	}
	
	return true
}

func (ad *AntiDRDoSManager) updateRateLimit(testerID string) {
	now := time.Now()
	limit, exists := ad.testRequestLimits[testerID]
	
	if !exists {
		ad.testRequestLimits[testerID] = &RateLimit{
			ValidatorID:   testerID,
			RequestCount:  1,
			WindowStart:   now,
			LastRequestAt: now,
		}
		return
	}
	
	if now.Sub(limit.WindowStart) > time.Hour {
		limit.RequestCount = 1
		limit.WindowStart = now
	} else {
		limit.RequestCount++
	}
	
	limit.LastRequestAt = now
}

func (ad *AntiDRDoSManager) CheckReputationThreshold(validatorReputation float64) bool {
	return validatorReputation >= ad.minReputation
}

func (ad *AntiDRDoSManager) GetRemainingTests(testerID string) int {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	
	limit, exists := ad.testRequestLimits[testerID]
	if !exists {
		return ad.maxRequestsPerHour
	}
	
	now := time.Now()
	if now.Sub(limit.WindowStart) > time.Hour {
		return ad.maxRequestsPerHour
	}
	
	remaining := ad.maxRequestsPerHour - limit.RequestCount
	if remaining < 0 {
		return 0
	}
	
	return remaining
}

func (ad *AntiDRDoSManager) CleanupExpiredChallenges() int {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	
	now := time.Now()
	removed := 0
	
	for id, challenge := range ad.activeChallenges {
		if now.After(challenge.ExpiresAt) || challenge.Completed {
			delete(ad.activeChallenges, id)
			removed++
		}
	}
	
	return removed
}

func (ad *AntiDRDoSManager) ComputeChallengeResponse(candidateID, testerID string, nonce []byte) string {
	combinedData := append([]byte(candidateID), nonce...)
	combinedData = append(combinedData, []byte(testerID)...)
	hash := sha256.Sum256(combinedData)
	return hex.EncodeToString(hash[:])
}

func (ad *AntiDRDoSManager) SetRateLimit(maxRequestsPerHour int) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.maxRequestsPerHour = maxRequestsPerHour
}

func (ad *AntiDRDoSManager) SetChallengeTTL(ttl time.Duration) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.challengeTTL = ttl
}

func (ad *AntiDRDoSManager) SetMinReputation(minRep float64) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.minReputation = minRep
}
