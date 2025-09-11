// Package poh implements the Proof of History sequencer for the RNR protocol
package poh

import (
    "crypto/sha256"
    "encoding/binary"
    "sync"
    "time"
)

// Sequence represents a single PoH sequence entry
type Sequence struct {
    PreviousHash [32]byte    // Hash of the previous sequence
    Count        uint64      // Monotonically increasing counter
    Timestamp    int64       // Unix timestamp in nanoseconds
    Data         []byte      // Optional data to be included in the sequence
}

// Sequencer manages the continuous PoH sequence generation
type Sequencer struct {
    mu            sync.Mutex
    currentHash   [32]byte
    counter       uint64
    startTime     time.Time
    hashesPerSec  uint64
}

// NewSequencer creates a new PoH sequencer instance
func NewSequencer(hashesPerSec uint64) *Sequencer {
    return &Sequencer{
        startTime:    time.Now(),
        hashesPerSec: hashesPerSec,
    }
}

// Record creates a new sequence entry with optional data
func (s *Sequencer) Record(data []byte) *Sequence {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Create new sequence entry
    seq := &Sequence{
        PreviousHash: s.currentHash,
        Count:        s.counter,
        Timestamp:    time.Now().UnixNano(),
        Data:        data,
    }

    // Update current hash
    hasher := sha256.New()
    hasher.Write(seq.PreviousHash[:])
    binary.Write(hasher, binary.LittleEndian, seq.Count)
    binary.Write(hasher, binary.LittleEndian, seq.Timestamp)
    if len(data) > 0 {
        hasher.Write(data)
    }
    
    copy(s.currentHash[:], hasher.Sum(nil))
    s.counter++

    return seq
}

// Verify checks if a sequence entry is valid
func (s *Sequencer) Verify(seq *Sequence, prevSeq *Sequence) bool {
    if prevSeq != nil {
        // Verify counter continuity
        if seq.Count != prevSeq.Count+1 {
            return false
        }

        // Verify timestamp is after previous
        if seq.Timestamp <= prevSeq.Timestamp {
            return false
        }

        // Verify previous hash matches
        if seq.PreviousHash != prevSeq.PreviousHash {
            return false
        }
    }

    // Verify hash computation
    hasher := sha256.New()
    hasher.Write(seq.PreviousHash[:])
    binary.Write(hasher, binary.LittleEndian, seq.Count)
    binary.Write(hasher, binary.LittleEndian, seq.Timestamp)
    if len(seq.Data) > 0 {
        hasher.Write(seq.Data)
    }

    var computedHash [32]byte
    copy(computedHash[:], hasher.Sum(nil))

    return computedHash == seq.PreviousHash
}

// GetCurrentHash returns the latest hash in the sequence
func (s *Sequencer) GetCurrentHash() [32]byte {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.currentHash
}

// GetCount returns the current sequence counter
func (s *Sequencer) GetCount() uint64 {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.counter
}