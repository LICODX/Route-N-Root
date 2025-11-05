package network

import (
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// RateLimiter implements token bucket rate limiting per peer
type RateLimiter struct {
	limits map[peer.ID]*PeerLimit
	mu     sync.RWMutex
	
	// Global rate limit configuration
	maxRequestsPerMinute int
	maxBytesPerSecond    int64
	burstSize            int
}

// PeerLimit tracks rate limit state for a single peer
type PeerLimit struct {
	PeerID            peer.ID
	Tokens            int
	LastRefill        time.Time
	BytesThisSecond   int64
	LastByteReset     time.Time
	ViolationCount    int
	LastViolation     time.Time
	IsBanned          bool
	BanExpiry         time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxRequestsPerMinute, burstSize int, maxBytesPerSecond int64) *RateLimiter {
	return &RateLimiter{
		limits:               make(map[peer.ID]*PeerLimit),
		maxRequestsPerMinute: maxRequestsPerMinute,
		maxBytesPerSecond:    maxBytesPerSecond,
		burstSize:            burstSize,
	}
}

// AllowRequest checks if a request from peer is allowed
// SECURITY: Token bucket algorithm prevents request spam
func (rl *RateLimiter) AllowRequest(peerID peer.ID) (bool, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, exists := rl.limits[peerID]
	if !exists {
		// First request from this peer - initialize
		limit = &PeerLimit{
			PeerID:          peerID,
			Tokens:          rl.burstSize,
			LastRefill:      time.Now(),
			LastByteReset:   time.Now(),
			ViolationCount:  0,
		}
		rl.limits[peerID] = limit
	}

	// SECURITY: Check if peer is banned
	if limit.IsBanned {
		if time.Now().Before(limit.BanExpiry) {
			return false, fmt.Errorf("peer %s is banned until %v", peerID.String()[:8], limit.BanExpiry)
		}
		// Ban expired, unban peer
		limit.IsBanned = false
		limit.ViolationCount = 0
	}

	// Refill tokens based on time elapsed
	now := time.Now()
	elapsed := now.Sub(limit.LastRefill)
	tokensToAdd := int(elapsed.Minutes() * float64(rl.maxRequestsPerMinute))
	
	if tokensToAdd > 0 {
		limit.Tokens += tokensToAdd
		if limit.Tokens > rl.burstSize {
			limit.Tokens = rl.burstSize
		}
		limit.LastRefill = now
	}

	// SECURITY: Check if peer has available tokens
	if limit.Tokens <= 0 {
		// Rate limit exceeded
		limit.ViolationCount++
		limit.LastViolation = now

		// SECURITY: Ban peer if too many violations
		if limit.ViolationCount >= 5 {
			limit.IsBanned = true
			limit.BanExpiry = now.Add(10 * time.Minute)
			return false, fmt.Errorf("peer %s banned for excessive rate limit violations", peerID.String()[:8])
		}

		return false, fmt.Errorf("rate limit exceeded for peer %s", peerID.String()[:8])
	}

	// Consume one token
	limit.Tokens--
	return true, nil
}

// AllowBytes checks if peer can send specified number of bytes
// SECURITY: Prevents bandwidth exhaustion attacks
func (rl *RateLimiter) AllowBytes(peerID peer.ID, bytes int64) (bool, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, exists := rl.limits[peerID]
	if !exists {
		return false, fmt.Errorf("peer %s not initialized", peerID.String()[:8])
	}

	now := time.Now()

	// Reset byte counter every second
	if now.Sub(limit.LastByteReset) > time.Second {
		limit.BytesThisSecond = 0
		limit.LastByteReset = now
	}

	// SECURITY: Check bandwidth limit
	if limit.BytesThisSecond+bytes > rl.maxBytesPerSecond {
		limit.ViolationCount++
		limit.LastViolation = now

		// Ban if excessive
		if limit.ViolationCount >= 3 {
			limit.IsBanned = true
			limit.BanExpiry = now.Add(5 * time.Minute)
			return false, fmt.Errorf("peer %s banned for bandwidth abuse", peerID.String()[:8])
		}

		return false, fmt.Errorf("bandwidth limit exceeded for peer %s", peerID.String()[:8])
	}

	// Allow bytes
	limit.BytesThisSecond += bytes
	return true, nil
}

// GetPeerLimit returns current rate limit state for peer
func (rl *RateLimiter) GetPeerLimit(peerID peer.ID) *PeerLimit {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.limits[peerID]
}

// ResetPeer resets rate limit for a peer (e.g., after successful auth)
func (rl *RateLimiter) ResetPeer(peerID peer.ID) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, exists := rl.limits[peerID]
	if exists {
		limit.Tokens = rl.burstSize
		limit.ViolationCount = 0
		limit.IsBanned = false
	}
}

// BanPeer immediately bans a peer
func (rl *RateLimiter) BanPeer(peerID peer.ID, duration time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limit, exists := rl.limits[peerID]
	if !exists {
		limit = &PeerLimit{
			PeerID: peerID,
		}
		rl.limits[peerID] = limit
	}

	limit.IsBanned = true
	limit.BanExpiry = time.Now().Add(duration)
}

// CleanupOldLimits removes rate limit data for peers not seen recently
func (rl *RateLimiter) CleanupOldLimits() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for peerID, limit := range rl.limits {
		// Remove if not seen in 10 minutes and not banned
		if !limit.IsBanned && now.Sub(limit.LastRefill) > 10*time.Minute {
			delete(rl.limits, peerID)
		}
	}
}

// StartCleanupRoutine starts background cleanup
func (rl *RateLimiter) StartCleanupRoutine() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			rl.CleanupOldLimits()
		}
	}()
}

// GetBannedPeers returns list of currently banned peers
func (rl *RateLimiter) GetBannedPeers() []peer.ID {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	banned := make([]peer.ID, 0)
	now := time.Now()
	for peerID, limit := range rl.limits {
		if limit.IsBanned && now.Before(limit.BanExpiry) {
			banned = append(banned, peerID)
		}
	}
	return banned
}
