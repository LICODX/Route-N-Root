package blockchain

import (
        "bytes"
        "encoding/json"
        "fmt"
        "log"
        "sync"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/core"
)

type ChainInfo struct {
        TipBlock   *core.Block
        TotalWork  uint64
        Height     uint64
        ChainID    string
}

type ForkResolver struct {
        db              *leveldb.DB
        mainChain       *Blockchain
        candidateChains map[string]*ChainInfo
        mu              sync.RWMutex
}

func NewForkResolver(bc *Blockchain, db *leveldb.DB) *ForkResolver {
        return &ForkResolver{
                db:              db,
                mainChain:       bc,
                candidateChains: make(map[string]*ChainInfo),
        }
}

func (fr *ForkResolver) HandleCompetingBlock(block *core.Block) error {
        fr.mu.Lock()
        defer fr.mu.Unlock()

        mainTip := fr.mainChain.GetLatestBlock()
        
        if bytes.Equal(block.Header.PrevBlockHash, fr.hashBlock(mainTip)) {
                return fr.mainChain.AddBlock(block)
        }

        chainID := fmt.Sprintf("fork_%x", block.Header.PrevBlockHash[:8])
        
        if chain, exists := fr.candidateChains[chainID]; exists {
                chain.TipBlock = block
                chain.Height = block.Header.Height
                chain.TotalWork += fr.calculateBlockWork(block)
                
                if fr.shouldReorg(chain, fr.getMainChainInfo()) {
                        return fr.reorganize(chain)
                }
                
                return nil
        }

        parentBlock, err := fr.findBlock(block.Header.PrevBlockHash)
        if err != nil {
                log.Printf("⚠️  Orphan block received: %d, saving for later", block.Header.Height)
                return fr.saveOrphanBlock(block)
        }

        newChain := &ChainInfo{
                TipBlock:  block,
                Height:    block.Header.Height,
                TotalWork: fr.calculateChainWork(parentBlock, block),
                ChainID:   chainID,
        }
        
        fr.candidateChains[chainID] = newChain

        if fr.shouldReorg(newChain, fr.getMainChainInfo()) {
                return fr.reorganize(newChain)
        }

        log.Printf("📊 Fork detected: main_height=%d, fork_height=%d, fork_id=%s",
                mainTip.Header.Height, newChain.Height, chainID[:8])

        return nil
}

func (fr *ForkResolver) shouldReorg(candidate *ChainInfo, main *ChainInfo) bool {
        // Whitepaper Bab 4.3: Fork resolution priority:
        // 1. Bobot PoB Kumulatif Tertinggi (Σ difficulty_target)
        // 2. Timestamp PoH Terkecil (if equal weight)
        // 3. Hash Blok Terkecil (final tiebreaker)
        
        // Safety: Always reorg if candidate significantly longer
        if candidate.Height > main.Height+6 {
                return true
        }

        // Priority 1: Higher cumulative PoB weight wins
        if candidate.TotalWork > main.TotalWork {
                return true
        }
        
        // Priority 2: If equal weight, earlier PoH timestamp wins
        if candidate.TotalWork == main.TotalWork {
                // PoHSequence is in Block, not BlockHeader
                if bytes.Compare(candidate.TipBlock.PoHSequence, main.TipBlock.PoHSequence) < 0 {
                        return true
                }
                
                // Priority 3: If same PoH timestamp, smaller hash wins
                if bytes.Equal(candidate.TipBlock.PoHSequence, main.TipBlock.PoHSequence) {
                        candidateHash, _ := candidate.TipBlock.Hash()
                        mainHash, _ := main.TipBlock.Hash()
                        
                        // Lexicographic comparison of hashes
                        if bytes.Compare(candidateHash, mainHash) < 0 {
                                return true
                        }
                }
        }

        return false
}

func (fr *ForkResolver) reorganize(newChain *ChainInfo) error {
        log.Printf("🔄 Starting chain reorganization to height %d", newChain.Height)

        commonAncestor, err := fr.findCommonAncestor(newChain.TipBlock)
        if err != nil {
                return fmt.Errorf("failed to find common ancestor: %w", err)
        }

        blocksToRemove, err := fr.getBlocksFrom(commonAncestor.Header.Height+1, fr.mainChain.GetLatestBlock().Header.Height)
        if err != nil {
                return err
        }

        blocksToAdd, err := fr.getChainBlocks(newChain)
        if err != nil {
                return err
        }

        for i := len(blocksToRemove) - 1; i >= 0; i-- {
                if err := fr.rollbackBlock(blocksToRemove[i]); err != nil {
                        return fmt.Errorf("rollback failed: %w", err)
                }
        }

        for _, block := range blocksToAdd {
                if err := fr.mainChain.AddBlock(block); err != nil {
                        return fmt.Errorf("failed to add block during reorg: %w", err)
                }
        }

        delete(fr.candidateChains, newChain.ChainID)

        log.Printf("✅ Chain reorganization complete! New tip: %d", newChain.Height)
        return nil
}

func (fr *ForkResolver) findCommonAncestor(forkTip *core.Block) (*core.Block, error) {
        currentForkBlock := forkTip
        mainTip := fr.mainChain.GetLatestBlock()

        for currentForkBlock.Header.Height > mainTip.Header.Height {
                parent, err := fr.findBlock(currentForkBlock.Header.PrevBlockHash)
                if err != nil {
                        return nil, err
                }
                currentForkBlock = parent
        }

        currentMainBlock := mainTip
        for currentMainBlock.Header.Height > currentForkBlock.Header.Height {
                parent, err := fr.findBlock(currentMainBlock.Header.PrevBlockHash)
                if err != nil {
                        return nil, err
                }
                currentMainBlock = parent
        }

        for {
                if bytes.Equal(fr.hashBlock(currentMainBlock), fr.hashBlock(currentForkBlock)) {
                        return currentMainBlock, nil
                }

                mainParent, err := fr.findBlock(currentMainBlock.Header.PrevBlockHash)
                if err != nil {
                        return nil, err
                }

                forkParent, err := fr.findBlock(currentForkBlock.Header.PrevBlockHash)
                if err != nil {
                        return nil, err
                }

                currentMainBlock = mainParent
                currentForkBlock = forkParent
        }
}

func (fr *ForkResolver) getChainBlocks(chain *ChainInfo) ([]*core.Block, error) {
        blocks := make([]*core.Block, 0)
        current := chain.TipBlock

        mainTip := fr.mainChain.GetLatestBlock()

        for current.Header.Height > mainTip.Header.Height {
                blocks = append([]*core.Block{current}, blocks...)
                parent, err := fr.findBlock(current.Header.PrevBlockHash)
                if err != nil {
                        return nil, err
                }
                current = parent
        }

        return blocks, nil
}

func (fr *ForkResolver) getBlocksFrom(startHeight, endHeight uint64) ([]*core.Block, error) {
        blocks := make([]*core.Block, 0)
        for height := startHeight; height <= endHeight; height++ {
                block, err := fr.mainChain.GetBlockByHeight(height)
                if err != nil {
                        return nil, err
                }
                blocks = append(blocks, block)
        }
        return blocks, nil
}

func (fr *ForkResolver) rollbackBlock(block *core.Block) error {
        key := fmt.Sprintf("block_%d", block.Header.Height)
        if err := fr.db.Delete([]byte(key), nil); err != nil {
                return err
        }

        log.Printf("↩️  Rolled back block #%d", block.Header.Height)
        return nil
}

func (fr *ForkResolver) findBlock(hash []byte) (*core.Block, error) {
        mainBlock := fr.mainChain.GetLatestBlock()
        current := mainBlock

        for {
                if bytes.Equal(fr.hashBlock(current), hash) {
                        return current, nil
                }

                if current.Header.Height == 0 {
                        break
                }

                parent, err := fr.mainChain.GetBlockByHeight(current.Header.Height - 1)
                if err != nil {
                        break
                }
                current = parent
        }

        for _, chain := range fr.candidateChains {
                if bytes.Equal(fr.hashBlock(chain.TipBlock), hash) {
                        return chain.TipBlock, nil
                }
        }

        return nil, fmt.Errorf("block not found")
}

func (fr *ForkResolver) saveOrphanBlock(block *core.Block) error {
        key := fmt.Sprintf("orphan_%x", fr.hashBlock(block)[:8])
        blockBytes, err := json.Marshal(block)
        if err != nil {
                return err
        }
        return fr.db.Put([]byte(key), blockBytes, nil)
}

func (fr *ForkResolver) getMainChainInfo() *ChainInfo {
        tip := fr.mainChain.GetLatestBlock()
        return &ChainInfo{
                TipBlock:  tip,
                Height:    tip.Header.Height,
                TotalWork: fr.calculateChainWork(nil, tip),
                ChainID:   "main",
        }
}

func (fr *ForkResolver) calculateBlockWork(block *core.Block) uint64 {
        // Whitepaper Bab 4.3: Use PoB weight as block work
        // Higher PoB score = more work contributed to chain security
        if block.Header.PoBWeight > 0 {
                return block.Header.PoBWeight
        }
        // Fallback for genesis or blocks without PoB weight
        return 1
}

func (fr *ForkResolver) calculateChainWork(from *core.Block, to *core.Block) uint64 {
        // Whitepaper Bab 4.3: Cumulative PoB weight (Σ difficulty_target)
        // Sum all PoB weights from blocks in range
        totalWork := uint64(0)
        current := to
        
        // Sum backwards from 'to' to 'from'
        for current.Header.Height >= from.Header.Height {
                totalWork += fr.calculateBlockWork(current)
                
                if current.Header.Height == from.Header.Height {
                        break
                }
                
                // Get parent block
                parent, err := fr.findBlock(current.Header.PrevBlockHash)
                if err != nil {
                        break
                }
                current = parent
        }
        
        return totalWork
}

func (fr *ForkResolver) hashBlock(block *core.Block) []byte {
        hash, _ := block.Hash()
        return hash
}

func (fr *ForkResolver) CleanupOldForks(maxAge uint64) {
        fr.mu.Lock()
        defer fr.mu.Unlock()

        mainHeight := fr.mainChain.GetLatestBlock().Header.Height

        for id, chain := range fr.candidateChains {
                if mainHeight-chain.Height > maxAge {
                        delete(fr.candidateChains, id)
                        log.Printf("🗑️  Cleaned up old fork: %s", id[:8])
                }
        }
}
