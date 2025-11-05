package blockchain

import (
        "bytes"
        "crypto/sha256"
        "encoding/json"
        "errors"
        "fmt"
        "math/big"
        "sync"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/core"
)

type Blockchain struct {
        db           *leveldb.DB
        currentBlock *core.Block
        Difficulty   *big.Int
        mu           sync.RWMutex
}

func NewBlockchain(db *leveldb.DB) (*Blockchain, error) {
        genesisBlock := createGenesisBlock()
        genesisBytes, err := json.Marshal(genesisBlock)
        if err != nil {
                return nil, err
        }

        currentBlockBytes, err := db.Get([]byte("current_block"), nil)
        if err != nil {
                currentBlockBytes = genesisBytes
                if err := db.Put([]byte("current_block"), currentBlockBytes, nil); err != nil {
                        return nil, err
                }
        }

        var currentBlock core.Block
        if err := json.Unmarshal(currentBlockBytes, &currentBlock); err != nil {
                return nil, err
        }

        return &Blockchain{
                db:           db,
                currentBlock: &currentBlock,
                Difficulty:   big.NewInt(1),
        }, nil
}

func createGenesisBlock() *core.Block {
        header := &core.BlockHeader{
                Version:       1,
                PrevBlockHash: []byte{},
                MerkleRoot:    []byte{},
                Timestamp:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
                Nonce:         0,
                Difficulty:    big.NewInt(1),
                Height:        0,
                PoBScore:      1.0,
        }
        return &core.Block{
                Header:       header,
                Transactions: []*core.Transaction{},
                ProposerID:   "genesis",
                PoHSequence:  []byte{},
                Signature:    []byte{},
        }
}

func (bc *Blockchain) GetLatestBlock() *core.Block {
        bc.mu.RLock()
        defer bc.mu.RUnlock()
        return bc.currentBlock
}

func (bc *Blockchain) AddBlock(block *core.Block) error {
        bc.mu.Lock()
        defer bc.mu.Unlock()

        blockBytes, err := json.Marshal(block)
        if err != nil {
                return err
        }

        key := fmt.Sprintf("block_%d", block.Header.Height)
        if err := bc.db.Put([]byte(key), blockBytes, nil); err != nil {
                return err
        }

        if err := bc.db.Put([]byte("current_block"), blockBytes, nil); err != nil {
                return err
        }

        bc.currentBlock = block
        return nil
}

func (bc *Blockchain) GetBlockByHeight(height uint64) (*core.Block, error) {
        bc.mu.RLock()
        defer bc.mu.RUnlock()

        key := fmt.Sprintf("block_%d", height)
        blockBytes, err := bc.db.Get([]byte(key), nil)
        if err != nil {
                return nil, fmt.Errorf("block not found at height %d: %w", height, err)
        }

        var block core.Block
        if err := json.Unmarshal(blockBytes, &block); err != nil {
                return nil, fmt.Errorf("failed to unmarshal block: %w", err)
        }

        return &block, nil
}

func (bc *Blockchain) VerifyBlock(block *core.Block) error {
        latestBlock := bc.GetLatestBlock()

        if block.Header.Height != latestBlock.Header.Height+1 {
                return errors.New("invalid block height")
        }

        prevHash, err := latestBlock.Hash()
        if err != nil {
                return err
        }
        if !bytes.Equal(block.Header.PrevBlockHash, prevHash) {
                return errors.New("invalid previous block hash")
        }

        return nil
}

// GetTransactionHistory returns transaction history for an address
func (bc *Blockchain) GetTransactionHistory(address string, limit int) []*core.Transaction {
        bc.mu.RLock()
        defer bc.mu.RUnlock()

        var transactions []*core.Transaction
        currentHeight := bc.currentBlock.Header.Height

        // Scan backwards from latest block
        for height := currentHeight; height >= 0 && len(transactions) < limit; height-- {
                block, err := bc.GetBlockByHeight(height)
                if err != nil {
                        break
                }

                for _, tx := range block.Transactions {
                        if tx.From == address || tx.To == address {
                                transactions = append(transactions, tx)
                                if len(transactions) >= limit {
                                        break
                                }
                        }
                }

                if height == 0 {
                        break
                }
        }

        return transactions
}

// GetTransactionByID finds a transaction by its ID across all blocks
func (bc *Blockchain) GetTransactionByID(txID string) *core.Transaction {
        bc.mu.RLock()
        defer bc.mu.RUnlock()

        currentHeight := bc.currentBlock.Header.Height

        // Search backwards from latest block
        for height := currentHeight; height >= 0; height-- {
                block, err := bc.GetBlockByHeight(height)
                if err != nil {
                        break
                }

                for _, tx := range block.Transactions {
                        if tx.ID == txID {
                                return tx
                        }
                }

                if height == 0 {
                        break
                }
        }

        return nil
}

// GetTotalTransactionCount returns total number of transactions in blockchain
func (bc *Blockchain) GetTotalTransactionCount() uint64 {
        bc.mu.RLock()
        defer bc.mu.RUnlock()

        var count uint64
        currentHeight := bc.currentBlock.Header.Height

        for height := uint64(0); height <= currentHeight; height++ {
                block, err := bc.GetBlockByHeight(height)
                if err != nil {
                        break
                }
                count += uint64(len(block.Transactions))
        }

        return count
}

func ComputeMerkleRoot(hashes [][]byte) ([]byte, error) {
        if len(hashes) == 0 {
                return []byte{}, nil
        }
        if len(hashes) == 1 {
                return hashes[0], nil
        }

        var newHashes [][]byte
        for i := 0; i < len(hashes); i += 2 {
                if i+1 < len(hashes) {
                        pairHash := sha256.Sum256(append(hashes[i], hashes[i+1]...))
                        newHashes = append(newHashes, pairHash[:])
                } else {
                        newHashes = append(newHashes, hashes[i])
                }
        }
        return ComputeMerkleRoot(newHashes)
}
