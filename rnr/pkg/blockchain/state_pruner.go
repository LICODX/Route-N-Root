package blockchain

import (
        "fmt"
        "log"
        "strconv"
        "strings"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "github.com/syndtr/goleveldb/leveldb/util"
)

// StatePruner handles database cleanup and pruning
type StatePruner struct {
        db              *leveldb.DB
        blockchain      *Blockchain
        checkpointMgr   interface{ GetLastFinalizedHeight() uint64 } // Checkpoint manager for finality
        retentionBlocks uint64        // Number of blocks to retain AFTER finalized checkpoint
        retentionTime   time.Duration // Time to retain transactions
}

// NewStatePruner creates a new state pruner
func NewStatePruner(db *leveldb.DB, blockchain *Blockchain, checkpointMgr interface{ GetLastFinalizedHeight() uint64 }, retentionBlocks uint64, retentionTime time.Duration) *StatePruner {
        return &StatePruner{
                db:              db,
                blockchain:      blockchain,
                checkpointMgr:   checkpointMgr,
                retentionBlocks: retentionBlocks,
                retentionTime:   retentionTime,
        }
}

// PruneOldBlocks removes blocks older than finalized checkpoint + retention
// SAFETY: Only prunes blocks AFTER checkpoint finality to preserve fork resolution
func (sp *StatePruner) PruneOldBlocks() (int, error) {
        // Get last finalized checkpoint height
        finalizedHeight := sp.checkpointMgr.GetLastFinalizedHeight()
        if finalizedHeight == 0 {
                // No finalized checkpoints yet, cannot prune safely
                log.Printf("📊 No finalized checkpoints - skipping pruning")
                return 0, nil
        }

        // Only prune blocks BEFORE (finalizedHeight - retentionBlocks)
        // This ensures we keep retentionBlocks AFTER the last finalized checkpoint
        if finalizedHeight <= sp.retentionBlocks {
                // Not enough finalized blocks to prune
                return 0, nil
        }

        pruneThreshold := finalizedHeight - sp.retentionBlocks
        prunedCount := 0

        // Iterate through all block keys
        iter := sp.db.NewIterator(util.BytesPrefix([]byte("block_")), nil)
        defer iter.Release()

        batch := new(leveldb.Batch)
        for iter.Next() {
                key := string(iter.Key())
                
                // Extract block height from key (format: "block_<height>")
                heightStr := strings.TrimPrefix(key, "block_")
                height, err := strconv.ParseUint(heightStr, 10, 64)
                if err != nil {
                        log.Printf("⚠️  Invalid block key format: %s", key)
                        continue
                }

                // Delete blocks below pruning threshold
                if height < pruneThreshold {
                        batch.Delete([]byte(key))
                        prunedCount++
                }

                // Flush batch every 100 deletions to avoid memory issues
                if batch.Len() >= 100 {
                        if err := sp.db.Write(batch, nil); err != nil {
                                return prunedCount, fmt.Errorf("failed to write batch: %w", err)
                        }
                        batch.Reset()
                }
        }

        // Final batch write - only if there are pending deletions
        if batch.Len() > 0 {
                if err := sp.db.Write(batch, nil); err != nil {
                        return prunedCount, fmt.Errorf("failed to write final batch: %w", err)
                }
        }

        if prunedCount > 0 {
                log.Printf("🗑️  Pruned %d old blocks below height %d (finalized: %d, retention: %d)", 
                        prunedCount, pruneThreshold, finalizedHeight, sp.retentionBlocks)
        }

        return prunedCount, iter.Error()
}

// PruneOldTransactions removes transactions older than retention time
// Note: This assumes transactions have timestamps or are associated with blocks
func (sp *StatePruner) PruneOldTransactions() (int, error) {
        // For now, we keep all transactions as they're used for verification
        // Future enhancement: Archive old transactions or prune after checkpoint finality
        
        // Placeholder: Count how many transactions exist
        iter := sp.db.NewIterator(util.BytesPrefix([]byte("tx_")), nil)
        defer iter.Release()

        count := 0
        for iter.Next() {
                count++
        }

        log.Printf("📊 Transaction count: %d (pruning deferred - waiting for archive strategy)", count)
        return 0, nil
}

// GetDatabaseStats returns statistics about database size
func (sp *StatePruner) GetDatabaseStats() map[string]interface{} {
        stats := make(map[string]interface{})

        // Count blocks
        blockIter := sp.db.NewIterator(util.BytesPrefix([]byte("block_")), nil)
        blockCount := 0
        for blockIter.Next() {
                blockCount++
        }
        blockIter.Release()

        // Count transactions
        txIter := sp.db.NewIterator(util.BytesPrefix([]byte("tx_")), nil)
        txCount := 0
        for txIter.Next() {
                txCount++
        }
        txIter.Release()

        // Count accounts
        accountIter := sp.db.NewIterator(util.BytesPrefix([]byte("account_")), nil)
        accountCount := 0
        for accountIter.Next() {
                accountCount++
        }
        accountIter.Release()

        // Count validators
        validatorIter := sp.db.NewIterator(util.BytesPrefix([]byte("validator_")), nil)
        validatorCount := 0
        for validatorIter.Next() {
                validatorCount++
        }
        validatorIter.Release()

        stats["blocks"] = blockCount
        stats["transactions"] = txCount
        stats["accounts"] = accountCount
        stats["validators"] = validatorCount
        stats["retention_blocks"] = sp.retentionBlocks
        stats["retention_time"] = sp.retentionTime.String()

        return stats
}

// CompactDatabase triggers LevelDB compaction to reclaim disk space
func (sp *StatePruner) CompactDatabase() error {
        log.Printf("🔧 Starting database compaction...")
        
        // Compact entire database range
        if err := sp.db.CompactRange(util.Range{Start: nil, Limit: nil}); err != nil {
                return fmt.Errorf("compaction failed: %w", err)
        }

        log.Printf("✅ Database compaction complete")
        return nil
}

// PerformMaintenance runs full maintenance cycle
func (sp *StatePruner) PerformMaintenance() error {
        log.Printf("🧹 Starting database maintenance...")

        // Prune old blocks
        blocksPruned, err := sp.PruneOldBlocks()
        if err != nil {
                return fmt.Errorf("block pruning failed: %w", err)
        }

        // Prune old transactions (currently no-op)
        _, err = sp.PruneOldTransactions()
        if err != nil {
                return fmt.Errorf("transaction pruning failed: %w", err)
        }

        // Compact database if blocks were pruned
        if blocksPruned > 0 {
                if err := sp.CompactDatabase(); err != nil {
                        log.Printf("⚠️  Compaction failed: %v", err)
                }
        }

        // Log stats
        stats := sp.GetDatabaseStats()
        log.Printf("📊 Database Stats: Blocks=%d, Transactions=%d, Accounts=%d, Validators=%d",
                stats["blocks"], stats["transactions"], stats["accounts"], stats["validators"])

        return nil
}
