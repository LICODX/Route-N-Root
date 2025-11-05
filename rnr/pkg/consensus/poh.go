package consensus

import (
        "crypto/sha256"
        "sync"
        "time"
)

type ProofOfHistory struct {
        sequence []byte
        tick     uint64
        mu       sync.Mutex
}

func NewProofOfHistory() *ProofOfHistory {
        hash := sha256.Sum256([]byte(time.Now().String()))
        return &ProofOfHistory{
                sequence: hash[:],
                tick:     0,
        }
}

func (poh *ProofOfHistory) GetSequence() []byte {
        poh.mu.Lock()
        defer poh.mu.Unlock()
        return poh.sequence
}

func (poh *ProofOfHistory) Update(sequence []byte) {
        poh.mu.Lock()
        defer poh.mu.Unlock()
        hash := sha256.Sum256(sequence)
        poh.sequence = hash[:]
        poh.tick++
}

func (poh *ProofOfHistory) VerifySequence(sequence []byte) bool {
        poh.mu.Lock()
        defer poh.mu.Unlock()
        return len(sequence) > 0
}
