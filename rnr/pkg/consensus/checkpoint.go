package consensus

import (
        "encoding/json"
        "fmt"
        "log"
        "sync"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/core"
)

type Checkpoint struct {
        Height          uint64    `json:"height"`
        BlockHash       []byte    `json:"block_hash"`
        StateRoot       []byte    `json:"state_root"`
        Timestamp       time.Time `json:"timestamp"`
        ValidatorVotes  int       `json:"validator_votes"`
        TotalValidators int       `json:"total_validators"`
}

type CheckpointManager struct {
        db                *leveldb.DB
        checkpoints       map[uint64]*Checkpoint
        finalizedHeight   uint64
        checkpointInterval uint64
        mu                sync.RWMutex
}

func NewCheckpointManager(db *leveldb.DB, interval uint64) *CheckpointManager {
        cm := &CheckpointManager{
                db:                db,
                checkpoints:       make(map[uint64]*Checkpoint),
                checkpointInterval: interval,
        }

        cm.loadCheckpoints()

        return cm
}

func (cm *CheckpointManager) loadCheckpoints() {
        finalizedHeightBytes, err := cm.db.Get([]byte("finalized_height"), nil)
        if err == nil {
                var height uint64
                if err := json.Unmarshal(finalizedHeightBytes, &height); err == nil {
                        cm.finalizedHeight = height
                }
        }

        iter := cm.db.NewIterator(nil, nil)
        defer iter.Release()

        for iter.Next() {
                key := string(iter.Key())
                if len(key) > 11 && key[:11] == "checkpoint_" {
                        var checkpoint Checkpoint
                        if err := json.Unmarshal(iter.Value(), &checkpoint); err == nil {
                                cm.checkpoints[checkpoint.Height] = &checkpoint
                        }
                }
        }

        log.Printf("📍 Loaded checkpoints: %d total, finalized height: %d",
                len(cm.checkpoints), cm.finalizedHeight)
}

func (cm *CheckpointManager) ShouldCreateCheckpoint(height uint64) bool {
        return height%cm.checkpointInterval == 0
}

// GetLastFinalizedHeight returns the last finalized checkpoint height
func (cm *CheckpointManager) GetLastFinalizedHeight() uint64 {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.finalizedHeight
}

func (cm *CheckpointManager) CreateCheckpoint(block *core.Block, validatorVotes int, totalValidators int) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        blockHash, err := block.Hash()
        if err != nil {
                return fmt.Errorf("failed to hash block: %w", err)
        }

        checkpoint := &Checkpoint{
                Height:          block.Header.Height,
                BlockHash:       blockHash,
                StateRoot:       block.Header.MerkleRoot,
                Timestamp:       time.Now(),
                ValidatorVotes:  validatorVotes,
                TotalValidators: totalValidators,
        }

        cm.checkpoints[checkpoint.Height] = checkpoint

        checkpointBytes, err := json.Marshal(checkpoint)
        if err != nil {
                return fmt.Errorf("failed to marshal checkpoint: %w", err)
        }

        key := fmt.Sprintf("checkpoint_%d", checkpoint.Height)
        if err := cm.db.Put([]byte(key), checkpointBytes, nil); err != nil {
                return fmt.Errorf("failed to save checkpoint: %w", err)
        }

        log.Printf("📍 Checkpoint created at height %d with %d/%d votes",
                checkpoint.Height, validatorVotes, totalValidators)

        return nil
}

func (cm *CheckpointManager) FinalizeCheckpoint(height uint64) error {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        checkpoint, exists := cm.checkpoints[height]
        if !exists {
                return fmt.Errorf("checkpoint not found at height %d", height)
        }

        requiredVotes := int(float64(checkpoint.TotalValidators) * 0.67)
        if checkpoint.ValidatorVotes < requiredVotes {
                return fmt.Errorf("insufficient votes for finalization: %d/%d",
                        checkpoint.ValidatorVotes, requiredVotes)
        }

        cm.finalizedHeight = height

        finalizedHeightBytes, err := json.Marshal(height)
        if err != nil {
                return err
        }

        if err := cm.db.Put([]byte("finalized_height"), finalizedHeightBytes, nil); err != nil {
                return fmt.Errorf("failed to save finalized height: %w", err)
        }

        log.Printf("✅ Checkpoint finalized at height %d", height)

        return nil
}

func (cm *CheckpointManager) GetFinalizedHeight() uint64 {
        cm.mu.RLock()
        defer cm.mu.RUnlock()
        return cm.finalizedHeight
}

func (cm *CheckpointManager) GetCheckpoint(height uint64) (*Checkpoint, error) {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        checkpoint, exists := cm.checkpoints[height]
        if !exists {
                return nil, fmt.Errorf("checkpoint not found at height %d", height)
        }

        return checkpoint, nil
}

func (cm *CheckpointManager) GetLatestCheckpoint() *Checkpoint {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        var latest *Checkpoint
        for _, cp := range cm.checkpoints {
                if latest == nil || cp.Height > latest.Height {
                        latest = cp
                }
        }

        return latest
}

func (cm *CheckpointManager) CanReorg(toHeight uint64) bool {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        return toHeight > cm.finalizedHeight
}

func (cm *CheckpointManager) CleanupOldCheckpoints(keepLast int) {
        cm.mu.Lock()
        defer cm.mu.Unlock()

        if len(cm.checkpoints) <= keepLast {
                return
        }

        heights := make([]uint64, 0, len(cm.checkpoints))
        for height := range cm.checkpoints {
                heights = append(heights, height)
        }

        for i := 0; i < len(heights)-1; i++ {
                for j := i + 1; j < len(heights); j++ {
                        if heights[i] < heights[j] {
                                heights[i], heights[j] = heights[j], heights[i]
                        }
                }
        }

        for i := keepLast; i < len(heights); i++ {
                height := heights[i]
                if height <= cm.finalizedHeight {
                        continue
                }

                delete(cm.checkpoints, height)
                key := fmt.Sprintf("checkpoint_%d", height)
                cm.db.Delete([]byte(key), nil)
        }

        log.Printf("🧹 Cleaned up old checkpoints, keeping last %d", keepLast)
}

func (cm *CheckpointManager) GetCheckpointStats() map[string]interface{} {
        cm.mu.RLock()
        defer cm.mu.RUnlock()

        return map[string]interface{}{
                "total_checkpoints": len(cm.checkpoints),
                "finalized_height":  cm.finalizedHeight,
                "latest_checkpoint": func() uint64 {
                        latest := cm.GetLatestCheckpoint()
                        if latest != nil {
                                return latest.Height
                        }
                        return 0
                }(),
        }
}
