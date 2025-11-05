package network

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// P2PAuthManager handles peer authentication via challenge-response protocol
type P2PAuthManager struct {
	challenges    map[peer.ID]*Challenge
	authenticated map[peer.ID]time.Time
	mu            sync.RWMutex
	challengeTTL  time.Duration
	authTimeout   time.Duration
}

// Challenge represents an authentication challenge sent to a peer
type Challenge struct {
	PeerID    peer.ID
	Nonce     []byte
	Timestamp time.Time
	Solved    bool
}

// ChallengeResponse is the peer's response to authentication challenge
type ChallengeResponse struct {
	PeerID    peer.ID
	Nonce     []byte
	Solution  []byte
	Signature []byte
	Timestamp time.Time
}

// NewP2PAuthManager creates a new authentication manager
func NewP2PAuthManager() *P2PAuthManager {
	return &P2PAuthManager{
		challenges:    make(map[peer.ID]*Challenge),
		authenticated: make(map[peer.ID]time.Time),
		challengeTTL:  5 * time.Minute,
		authTimeout:   30 * time.Second,
	}
}

// GenerateChallenge creates a cryptographic challenge for peer authentication
// SECURITY: Uses crypto/rand for unpredictable nonces
func (am *P2PAuthManager) GenerateChallenge(peerID peer.ID) (*Challenge, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Generate cryptographically secure random nonce
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate challenge nonce: %w", err)
	}

	challenge := &Challenge{
		PeerID:    peerID,
		Nonce:     nonce,
		Timestamp: time.Now(),
		Solved:    false,
	}

	am.challenges[peerID] = challenge
	return challenge, nil
}

// VerifyResponse verifies peer's response to authentication challenge
// SECURITY: Checks nonce validity, timestamp freshness, and solution correctness
func (am *P2PAuthManager) VerifyResponse(response *ChallengeResponse) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	// Check if challenge exists
	challenge, exists := am.challenges[response.PeerID]
	if !exists {
		return fmt.Errorf("no challenge found for peer %s", response.PeerID.String()[:8])
	}

	// SECURITY: Check challenge has not expired
	if time.Since(challenge.Timestamp) > am.authTimeout {
		delete(am.challenges, response.PeerID)
		return fmt.Errorf("challenge expired for peer %s", response.PeerID.String()[:8])
	}

	// SECURITY: Verify nonce matches
	if hex.EncodeToString(challenge.Nonce) != hex.EncodeToString(response.Nonce) {
		return fmt.Errorf("nonce mismatch for peer %s", response.PeerID.String()[:8])
	}

	// SECURITY: Verify solution is correct
	// Expected solution: SHA256(nonce || peerID)
	expectedSolution := sha256.Sum256(append(challenge.Nonce, []byte(response.PeerID.String())...))
	if hex.EncodeToString(expectedSolution[:]) != hex.EncodeToString(response.Solution) {
		return fmt.Errorf("invalid solution from peer %s", response.PeerID.String()[:8])
	}

	// SECURITY: Mark challenge as solved
	challenge.Solved = true

	// SECURITY: Mark peer as authenticated
	am.authenticated[response.PeerID] = time.Now()

	// Clean up challenge
	delete(am.challenges, response.PeerID)

	return nil
}

// IsAuthenticated checks if a peer has been authenticated
// SECURITY: Returns false if authentication has expired
func (am *P2PAuthManager) IsAuthenticated(peerID peer.ID) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()

	authTime, exists := am.authenticated[peerID]
	if !exists {
		return false
	}

	// SECURITY: Check if authentication is still valid (TTL)
	if time.Since(authTime) > am.challengeTTL {
		// Authentication expired
		return false
	}

	return true
}

// RevokeAuthentication removes authentication for a peer
// Used when peer misbehaves or disconnects
func (am *P2PAuthManager) RevokeAuthentication(peerID peer.ID) {
	am.mu.Lock()
	defer am.mu.Unlock()

	delete(am.authenticated, peerID)
	delete(am.challenges, peerID)
}

// SolveChallenge solves an authentication challenge
// This is called by the peer receiving a challenge
func (am *P2PAuthManager) SolveChallenge(peerID peer.ID, nonce []byte) []byte {
	// Solution: SHA256(nonce || peerID)
	solution := sha256.Sum256(append(nonce, []byte(peerID.String())...))
	return solution[:]
}

// CleanupExpired removes expired challenges and authentications
func (am *P2PAuthManager) CleanupExpired() {
	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now()

	// Clean up expired challenges
	for peerID, challenge := range am.challenges {
		if now.Sub(challenge.Timestamp) > am.authTimeout {
			delete(am.challenges, peerID)
		}
	}

	// Clean up expired authentications
	for peerID, authTime := range am.authenticated {
		if now.Sub(authTime) > am.challengeTTL {
			delete(am.authenticated, peerID)
		}
	}
}

// GetAuthenticatedPeers returns list of currently authenticated peers
func (am *P2PAuthManager) GetAuthenticatedPeers() []peer.ID {
	am.mu.RLock()
	defer am.mu.RUnlock()

	peers := make([]peer.ID, 0, len(am.authenticated))
	for peerID := range am.authenticated {
		peers = append(peers, peerID)
	}
	return peers
}

// StartCleanupRoutine starts background cleanup of expired challenges/auths
func (am *P2PAuthManager) StartCleanupRoutine() {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		for range ticker.C {
			am.CleanupExpired()
		}
	}()
}
