package consensus

import (
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "log"

        "github.com/syndtr/goleveldb/leveldb/util"
)

// generateVoteID creates a deterministic ID for vote replay prevention
// SECURITY CRITICAL: Uses ONLY blockHash + validatorID (NO signature!)
// Including signature would allow replay attacks via re-signing with different nonce
func generateVoteID(blockHash []byte, validatorID string) string {
        hasher := sha256.New()
        hasher.Write(blockHash)
        hasher.Write([]byte(validatorID))
        return hex.EncodeToString(hasher.Sum(nil))
}

// MarkVoteProcessed marks a vote as processed to prevent replay
func (vm *VotingManager) MarkVoteProcessed(voteID string) {
        vm.mu.Lock()
        defer vm.mu.Unlock()
        vm.processedVotes[voteID] = true
}

// IsVoteProcessed checks if a vote has already been processed
func (vm *VotingManager) IsVoteProcessed(blockHash []byte, validatorID string) bool {
        voteID := generateVoteID(blockHash, validatorID)
        vm.mu.RLock()
        defer vm.mu.RUnlock()
        return vm.processedVotes[voteID]
}

// CleanupOldVotes removes votes older than specified block height to prevent memory leak
// Should be called periodically (e.g., after finality checkpoint)
// ENHANCED: Also deletes votes from LevelDB for complete cleanup
func (vm *VotingManager) CleanupOldVotes(currentBlockHeight uint64, keepRecentBlocks uint64) int {
        vm.mu.Lock()
        defer vm.mu.Unlock()

        // Clean up old sessions (older than keepRecentBlocks)
        cleaned := 0
        for blockHashStr, session := range vm.sessions {
                session.mu.RLock()
                isOld := session.IsFinalized
                session.mu.RUnlock()

                if isOld {
                        // Remove votes from processedVotes map AND LevelDB
                        for _, vote := range session.Votes {
                                voteID := generateVoteID(vote.BlockHash, vote.ValidatorID)
                                delete(vm.processedVotes, voteID)
                                
                                // Delete from LevelDB
                                if err := vm.DeleteVoteFromDB(voteID); err != nil {
                                        log.Printf("⚠️  Failed to delete vote %s from DB: %v", voteID[:8], err)
                                }
                                
                                cleaned++
                        }
                        delete(vm.sessions, blockHashStr)
                }
        }

        log.Printf("🧹 Cleaned up %d old votes (memory + database)", cleaned)
        return cleaned
}

// PersistVote writes vote to persistent storage (LevelDB)
// This ensures votes survive node restarts and can be verified later
func (vm *VotingManager) PersistVote(vote *Vote) error {
        if vm.db == nil {
                return fmt.Errorf("database not initialized")
        }
        
        voteBytes, err := json.Marshal(vote)
        if err != nil {
                return fmt.Errorf("failed to marshal vote: %w", err)
        }
        
        key := fmt.Sprintf("vote_%s", vote.VoteID)
        if err := vm.db.Put([]byte(key), voteBytes, nil); err != nil {
                return fmt.Errorf("failed to persist vote: %w", err)
        }
        
        return nil
}

// LoadProcessedVotes loads all processed votes from LevelDB on startup
// MIGRATION: Recomputes deterministic VoteIDs for legacy votes
func (vm *VotingManager) LoadProcessedVotes() error {
        if vm.db == nil {
                return fmt.Errorf("database not initialized")
        }
        
        vm.mu.Lock()
        defer vm.mu.Unlock()
        
        iter := vm.db.NewIterator(util.BytesPrefix([]byte("vote_")), nil)
        defer iter.Release()
        
        loadedCount := 0
        migratedCount := 0
        
        for iter.Next() {
                var vote Vote
                if err := json.Unmarshal(iter.Value(), &vote); err != nil {
                        log.Printf("⚠️  Failed to unmarshal vote: %v", err)
                        continue
                }
                
                // MIGRATION: Recompute deterministic VoteID (without signature)
                correctVoteID := generateVoteID(vote.BlockHash, vote.ValidatorID)
                
                // Mark both legacy ID and correct ID as processed for safety
                vm.processedVotes[vote.VoteID] = true // Legacy ID
                vm.processedVotes[correctVoteID] = true // Deterministic ID
                
                // If IDs differ, this was a legacy vote - migrate it
                if vote.VoteID != correctVoteID {
                        // Delete legacy entry
                        legacyKey := fmt.Sprintf("vote_%s", vote.VoteID)
                        vm.db.Delete([]byte(legacyKey), nil)
                        
                        // Write with correct ID
                        vote.VoteID = correctVoteID
                        newKey := fmt.Sprintf("vote_%s", correctVoteID)
                        voteBytes, _ := json.Marshal(vote)
                        vm.db.Put([]byte(newKey), voteBytes, nil)
                        
                        migratedCount++
                }
                
                loadedCount++
        }
        
        if err := iter.Error(); err != nil {
                return fmt.Errorf("error iterating votes: %w", err)
        }
        
        if migratedCount > 0 {
                log.Printf("🔄 Migrated %d legacy votes to deterministic IDs", migratedCount)
        }
        log.Printf("✅ Loaded %d processed votes from database", loadedCount)
        return nil
}

// DeleteVoteFromDB removes a vote from persistent storage
func (vm *VotingManager) DeleteVoteFromDB(voteID string) error {
        if vm.db == nil {
                return nil // Silently skip if no database
        }
        
        key := fmt.Sprintf("vote_%s", voteID)
        if err := vm.db.Delete([]byte(key), nil); err != nil {
                return fmt.Errorf("failed to delete vote: %w", err)
        }
        
        return nil
}
