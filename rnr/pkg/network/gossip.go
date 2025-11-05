package network

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

type GossipMessage struct {
	MessageID   string                 `json:"message_id"`
	Type        string                 `json:"type"`
	Payload     []byte                 `json:"payload"`
	TTL         int                    `json:"ttl"`
	Timestamp   time.Time              `json:"timestamp"`
	SenderID    string                 `json:"sender_id"`
	Metadata    map[string]interface{} `json:"metadata"`
}

type GossipProtocol struct {
	seenMessages  map[string]time.Time
	mu            sync.RWMutex
	fanout        int
	maxTTL        int
	cleanupPeriod time.Duration
}

func NewGossipProtocol(fanout, maxTTL int) *GossipProtocol {
	gp := &GossipProtocol{
		seenMessages:  make(map[string]time.Time),
		fanout:        fanout,
		maxTTL:        maxTTL,
		cleanupPeriod: 5 * time.Minute,
	}

	go gp.cleanupLoop()

	return gp
}

func (gp *GossipProtocol) CreateMessage(msgType string, payload []byte, senderID string) *GossipMessage {
	return &GossipMessage{
		MessageID: generateMessageID(),
		Type:      msgType,
		Payload:   payload,
		TTL:       gp.maxTTL,
		Timestamp: time.Now(),
		SenderID:  senderID,
		Metadata:  make(map[string]interface{}),
	}
}

func (gp *GossipProtocol) ShouldProcess(msg *GossipMessage) bool {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	if _, seen := gp.seenMessages[msg.MessageID]; seen {
		return false
	}

	if msg.TTL <= 0 {
		return false
	}

	if time.Since(msg.Timestamp) > 1*time.Minute {
		return false
	}

	gp.seenMessages[msg.MessageID] = time.Now()
	return true
}

func (gp *GossipProtocol) DecrementTTL(msg *GossipMessage) *GossipMessage {
	msg.TTL--
	return msg
}

func (gp *GossipProtocol) SelectPeersForGossip(allPeers []peer.ID, excludePeer peer.ID) []peer.ID {
	available := make([]peer.ID, 0)
	for _, p := range allPeers {
		if p != excludePeer {
			available = append(available, p)
		}
	}

	if len(available) <= gp.fanout {
		return available
	}

	selected := make([]peer.ID, gp.fanout)
	used := make(map[int]bool)

	for i := 0; i < gp.fanout; i++ {
		for {
			idx := randomInt(len(available))
			if !used[idx] {
				selected[i] = available[idx]
				used[idx] = true
				break
			}
		}
	}

	return selected
}

func (gp *GossipProtocol) cleanupLoop() {
	ticker := time.NewTicker(gp.cleanupPeriod)
	defer ticker.Stop()

	for range ticker.C {
		gp.cleanupOldMessages()
	}
}

func (gp *GossipProtocol) cleanupOldMessages() {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)

	for msgID, timestamp := range gp.seenMessages {
		if timestamp.Before(cutoff) {
			delete(gp.seenMessages, msgID)
		}
	}

	log.Printf("🧹 Gossip cleanup: %d seen messages", len(gp.seenMessages))
}

func (gp *GossipProtocol) GetStats() map[string]interface{} {
	gp.mu.RLock()
	defer gp.mu.RUnlock()

	return map[string]interface{}{
		"seen_messages": len(gp.seenMessages),
		"fanout":        gp.fanout,
		"max_ttl":       gp.maxTTL,
	}
}

func (msg *GossipMessage) Marshal() ([]byte, error) {
	return json.Marshal(msg)
}

func UnmarshalGossipMessage(data []byte) (*GossipMessage, error) {
	var msg GossipMessage
	err := json.Unmarshal(data, &msg)
	return &msg, err
}

func generateMessageID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func randomInt(max int) int {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(nBig.Int64())
}
