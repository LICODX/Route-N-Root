package consensus

import (
	"log"
	"sync"
	"time"
)

type PartitionStatus string

const (
	StatusHealthy     PartitionStatus = "healthy"
	StatusSuspected   PartitionStatus = "suspected"
	StatusPartitioned PartitionStatus = "partitioned"
)

type PartitionDetector struct {
	peerHeartbeats    map[string]time.Time
	totalValidators   int
	connectedPeers    int
	status            PartitionStatus
	mu                sync.RWMutex
	heartbeatTimeout  time.Duration
	partitionThreshold float64
}

func NewPartitionDetector(totalValidators int) *PartitionDetector {
	return &PartitionDetector{
		peerHeartbeats:     make(map[string]time.Time),
		totalValidators:    totalValidators,
		status:             StatusHealthy,
		heartbeatTimeout:   30 * time.Second,
		partitionThreshold: 0.33,
	}
}

func (pd *PartitionDetector) UpdateHeartbeat(peerID string) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.peerHeartbeats[peerID] = time.Now()
}

func (pd *PartitionDetector) CheckPartitionStatus() PartitionStatus {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	now := time.Now()
	activeCount := 0

	for peerID, lastSeen := range pd.peerHeartbeats {
		if now.Sub(lastSeen) > pd.heartbeatTimeout {
			delete(pd.peerHeartbeats, peerID)
		} else {
			activeCount++
		}
	}

	pd.connectedPeers = activeCount

	connectedRatio := float64(activeCount) / float64(pd.totalValidators)

	if connectedRatio < pd.partitionThreshold {
		if pd.status != StatusPartitioned {
			pd.status = StatusPartitioned
			log.Printf("⚠️  NETWORK PARTITION DETECTED! Connected: %d/%d validators",
				activeCount, pd.totalValidators)
		}
		return StatusPartitioned
	}

	if connectedRatio < 0.67 {
		if pd.status != StatusSuspected {
			pd.status = StatusSuspected
			log.Printf("⚠️  Network partition suspected. Connected: %d/%d validators",
				activeCount, pd.totalValidators)
		}
		return StatusSuspected
	}

	if pd.status != StatusHealthy {
		pd.status = StatusHealthy
		log.Printf("✅ Network partition resolved. Connected: %d/%d validators",
			activeCount, pd.totalValidators)
	}

	return StatusHealthy
}

func (pd *PartitionDetector) ShouldHaltConsensus() bool {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	return pd.status == StatusPartitioned
}

func (pd *PartitionDetector) GetConnectedPeerCount() int {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.connectedPeers
}

func (pd *PartitionDetector) GetStatus() PartitionStatus {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.status
}
