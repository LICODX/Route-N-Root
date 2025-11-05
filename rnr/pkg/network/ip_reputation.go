package network

import (
        "net"
        "sync"
        "time"
)

// SECURITY: IP Reputation System untuk detect dan block malicious IPs
// Ini adalah 1% missing security untuk prevent IP-based attacks

type IPReputationScore int

const (
        ReputationGood IPReputationScore = iota
        ReputationSuspicious
        ReputationBad
        ReputationBlacklisted
)

type IPMisbehavior struct {
        Type        string    // "failed_auth", "rate_limit_exceeded", "invalid_vrf", "speedtest_cheat", "byzantine"
        Severity    int       // 1-10, where 10 is most severe
        Timestamp   time.Time
        Description string
}

type IPReputation struct {
        IP                net.IP
        Score             int // 0-100, where 100 is perfect reputation
        Misbehaviors      []IPMisbehavior
        FirstSeen         time.Time
        LastSeen          time.Time
        SuccessfulActions int
        FailedActions     int
        IsBlacklisted     bool
        BlacklistReason   string
        BlacklistUntil    time.Time
}

type IPReputationSystem struct {
        mu          sync.RWMutex
        reputations map[string]*IPReputation // IP string -> reputation
        
        // Configuration
        initialScore          int
        misbehaviorDecay      float64 // How fast reputation recovers
        blacklistThreshold    int     // Score below this = blacklist
        blacklistDuration     time.Duration
        
        // Sybil detection
        maxNodesPerSubnet     int
        subnetMask            int // /24 by default
        subnetNodeCount       map[string]int // subnet -> count
}

func NewIPReputationSystem() *IPReputationSystem {
        return &IPReputationSystem{
                reputations:        make(map[string]*IPReputation),
                initialScore:       100,
                misbehaviorDecay:   0.1,  // 10% recovery per hour
                blacklistThreshold: 20,   // Below 20/100 = blacklist
                blacklistDuration:  24 * time.Hour,
                maxNodesPerSubnet:  3,    // Max 3 nodes per /24 subnet (Sybil resistance)
                subnetMask:        24,
                subnetNodeCount:   make(map[string]int),
        }
}

// CheckIPAllowed checks if IP is allowed to connect
// SECURITY: Primary defense against malicious IPs
func (irs *IPReputationSystem) CheckIPAllowed(ip net.IP) (bool, string) {
        irs.mu.RLock()
        defer irs.mu.RUnlock()
        
        ipStr := ip.String()
        rep, exists := irs.reputations[ipStr]
        
        if !exists {
                // New IP - check Sybil resistance
                subnet := irs.getSubnet(ip)
                if irs.subnetNodeCount[subnet] >= irs.maxNodesPerSubnet {
                        return false, "Sybil attack detected: too many nodes from same subnet"
                }
                return true, ""
        }
        
        // Check blacklist
        if rep.IsBlacklisted {
                if time.Now().Before(rep.BlacklistUntil) {
                        return false, "IP blacklisted: " + rep.BlacklistReason
                }
                // Blacklist expired - allow but with low reputation
                rep.IsBlacklisted = false
                rep.Score = irs.blacklistThreshold + 5
        }
        
        // Check reputation score
        if rep.Score < irs.blacklistThreshold {
                return false, "IP reputation too low"
        }
        
        return true, ""
}

// RecordMisbehavior records malicious behavior from an IP
// SECURITY: Core function for tracking attacks
func (irs *IPReputationSystem) RecordMisbehavior(ip net.IP, misbehaviorType string, severity int, description string) {
        irs.mu.Lock()
        defer irs.mu.Unlock()
        
        rep := irs.getOrCreateReputation(ip)
        
        // Record misbehavior
        misbehavior := IPMisbehavior{
                Type:        misbehaviorType,
                Severity:    severity,
                Timestamp:   time.Now(),
                Description: description,
        }
        rep.Misbehaviors = append(rep.Misbehaviors, misbehavior)
        rep.FailedActions++
        rep.LastSeen = time.Now()
        
        // Decrease reputation based on severity
        // Severity 1-3: minor (-5 to -15)
        // Severity 4-7: moderate (-20 to -35)
        // Severity 8-10: critical (-40 to -50)
        penalty := severity * 5
        rep.Score -= penalty
        
        if rep.Score < 0 {
                rep.Score = 0
        }
        
        // Auto-blacklist if score too low
        if rep.Score < irs.blacklistThreshold && !rep.IsBlacklisted {
                rep.IsBlacklisted = true
                rep.BlacklistReason = "Accumulated misbehavior: " + misbehaviorType
                rep.BlacklistUntil = time.Now().Add(irs.blacklistDuration)
        }
        
        // CRITICAL: Auto-blacklist for critical attacks immediately
        if severity >= 8 {
                rep.IsBlacklisted = true
                rep.BlacklistReason = "Critical attack: " + description
                rep.BlacklistUntil = time.Now().Add(7 * 24 * time.Hour) // 7 days for critical
        }
}

// RecordSuccess records successful legitimate action
func (irs *IPReputationSystem) RecordSuccess(ip net.IP, actionType string) {
        irs.mu.Lock()
        defer irs.mu.Unlock()
        
        rep := irs.getOrCreateReputation(ip)
        rep.SuccessfulActions++
        rep.LastSeen = time.Now()
        
        // Increase reputation (but cap at 100)
        rep.Score += 1
        if rep.Score > 100 {
                rep.Score = 100
        }
}

// GetReputation returns current reputation for an IP
func (irs *IPReputationSystem) GetReputation(ip net.IP) *IPReputation {
        irs.mu.RLock()
        defer irs.mu.RUnlock()
        
        return irs.reputations[ip.String()]
}

// getOrCreateReputation internal helper
func (irs *IPReputationSystem) getOrCreateReputation(ip net.IP) *IPReputation {
        ipStr := ip.String()
        rep, exists := irs.reputations[ipStr]
        
        if !exists {
                rep = &IPReputation{
                        IP:           ip,
                        Score:        irs.initialScore,
                        Misbehaviors: make([]IPMisbehavior, 0),
                        FirstSeen:    time.Now(),
                        LastSeen:     time.Now(),
                }
                irs.reputations[ipStr] = rep
                
                // Track subnet count (Sybil resistance)
                subnet := irs.getSubnet(ip)
                irs.subnetNodeCount[subnet]++
        }
        
        return rep
}

// getSubnet extracts subnet for Sybil detection
func (irs *IPReputationSystem) getSubnet(ip net.IP) string {
        // For IPv4, use /24 by default
        if ip.To4() != nil {
                mask := net.CIDRMask(irs.subnetMask, 32)
                subnet := ip.Mask(mask)
                return subnet.String()
        }
        // For IPv6, use /64
        mask := net.CIDRMask(64, 128)
        subnet := ip.Mask(mask)
        return subnet.String()
}

// StartReputationDecay starts background process to recover reputation over time
func (irs *IPReputationSystem) StartReputationDecay() {
        go func() {
                ticker := time.NewTicker(1 * time.Hour)
                defer ticker.Stop()
                
                for range ticker.C {
                        irs.mu.Lock()
                        
                        for _, rep := range irs.reputations {
                                // Don't recover blacklisted IPs
                                if rep.IsBlacklisted && time.Now().Before(rep.BlacklistUntil) {
                                        continue
                                }
                                
                                // Gradual reputation recovery if no recent misbehavior
                                recentMisbehavior := false
                                cutoff := time.Now().Add(-24 * time.Hour)
                                
                                for _, mb := range rep.Misbehaviors {
                                        if mb.Timestamp.After(cutoff) {
                                                recentMisbehavior = true
                                                break
                                        }
                                }
                                
                                // Recover reputation if clean for 24 hours
                                if !recentMisbehavior && rep.Score < irs.initialScore {
                                        recovery := int(float64(irs.initialScore-rep.Score) * irs.misbehaviorDecay)
                                        if recovery < 1 {
                                                recovery = 1
                                        }
                                        rep.Score += recovery
                                        
                                        if rep.Score > irs.initialScore {
                                                rep.Score = irs.initialScore
                                        }
                                }
                        }
                        
                        irs.mu.Unlock()
                }
        }()
}

// CleanupOldReputations removes old reputation data
func (irs *IPReputationSystem) CleanupOldReputations() {
        irs.mu.Lock()
        defer irs.mu.Unlock()
        
        cutoff := time.Now().Add(-30 * 24 * time.Hour) // 30 days
        
        for ipStr, rep := range irs.reputations {
                // Keep blacklisted and recently seen IPs
                if rep.IsBlacklisted || rep.LastSeen.After(cutoff) {
                        continue
                }
                
                // Remove old clean IPs
                if rep.Score >= 90 && rep.LastSeen.Before(cutoff) {
                        subnet := irs.getSubnet(rep.IP)
                        irs.subnetNodeCount[subnet]--
                        if irs.subnetNodeCount[subnet] <= 0 {
                                delete(irs.subnetNodeCount, subnet)
                        }
                        delete(irs.reputations, ipStr)
                }
        }
}

// GetReputationStatus returns human-readable status
func (irs *IPReputationSystem) GetReputationStatus(ip net.IP) IPReputationScore {
        irs.mu.RLock()
        defer irs.mu.RUnlock()
        
        rep := irs.reputations[ip.String()]
        if rep == nil {
                return ReputationGood // New IPs start as good
        }
        
        if rep.IsBlacklisted {
                return ReputationBlacklisted
        }
        
        if rep.Score >= 80 {
                return ReputationGood
        } else if rep.Score >= 50 {
                return ReputationSuspicious
        } else {
                return ReputationBad
        }
}
