package consensus

import (
        "crypto/sha256"
        "fmt"
        "sync"

        "rnr-blockchain/pkg/core"
)

type ValidationResult struct {
        TxID  string
        Valid bool
        Error error
}

func ValidateTransactionsConcurrently(txs []*core.Transaction, state interface {
        GetAccount(address string) (*core.Account, error)
}) []ValidationResult {
        results := make([]ValidationResult, len(txs))
        var wg sync.WaitGroup
        
        for i, tx := range txs {
                wg.Add(1)
                go func(idx int, transaction *core.Transaction) {
                        defer wg.Done()
                        
                        result := ValidationResult{
                                TxID:  transaction.ID,
                                Valid: false,
                        }
                        
                        sender, err := state.GetAccount(transaction.From)
                        if err != nil {
                                result.Error = fmt.Errorf("failed to get sender account: %w", err)
                                results[idx] = result
                                return
                        }
                        
                        if sender.Balance.Cmp(transaction.Amount) < 0 {
                                result.Error = fmt.Errorf("insufficient balance")
                                results[idx] = result
                                return
                        }
                        
                        if sender.Nonce >= transaction.Nonce {
                                result.Error = fmt.Errorf("invalid nonce: expected > %d, got %d", sender.Nonce, transaction.Nonce)
                                results[idx] = result
                                return
                        }
                        
                        result.Valid = true
                        results[idx] = result
                }(i, tx)
        }
        
        wg.Wait()
        return results
}

func FilterValidTransactions(txs []*core.Transaction, results []ValidationResult) []*core.Transaction {
        validTxs := make([]*core.Transaction, 0)
        
        for i, result := range results {
                if result.Valid {
                        validTxs = append(validTxs, txs[i])
                }
        }
        
        return validTxs
}

type MerkleWorker struct {
        hashes chan []byte
        errors chan error
}

func ComputeMerkleRootConcurrent(txs []*core.Transaction) ([]byte, error) {
        if len(txs) == 0 {
                return []byte{}, nil
        }
        
        hashes := make([][]byte, len(txs))
        var wg sync.WaitGroup
        errorsChan := make(chan error, len(txs))
        
        for i, tx := range txs {
                wg.Add(1)
                go func(idx int, transaction *core.Transaction) {
                        defer wg.Done()
                        
                        hash, err := transaction.Hash()
                        if err != nil {
                                errorsChan <- err
                                return
                        }
                        
                        hashes[idx] = hash
                }(i, tx)
        }
        
        wg.Wait()
        close(errorsChan)
        
        if len(errorsChan) > 0 {
                return nil, <-errorsChan
        }
        
        return computeMerkleTree(hashes), nil
}

func computeMerkleTree(hashes [][]byte) []byte {
        if len(hashes) == 0 {
                return []byte{}
        }
        
        if len(hashes) == 1 {
                return hashes[0]
        }
        
        var newHashes [][]byte
        for i := 0; i < len(hashes); i += 2 {
                if i+1 < len(hashes) {
                        combined := append(hashes[i], hashes[i+1]...)
                        hash := sha256.Sum256(combined)
                        newHashes = append(newHashes, hash[:])
                } else {
                        newHashes = append(newHashes, hashes[i])
                }
        }
        
        return computeMerkleTree(newHashes)
}

type ParallelBlockValidator struct {
        workers int
}

func NewParallelBlockValidator(workers int) *ParallelBlockValidator {
        if workers <= 0 {
                workers = 4
        }
        
        return &ParallelBlockValidator{
                workers: workers,
        }
}

func (v *ParallelBlockValidator) ValidateBlockParallel(
        block *core.Block,
        state interface {
                GetAccount(address string) (*core.Account, error)
        },
        pohVerifier func([]byte) bool,
        prevBlockHash []byte,
) error {
        errorsChan := make(chan error, 3)
        var wg sync.WaitGroup
        
        wg.Add(1)
        go func() {
                defer wg.Done()
                if block.Header.Height == 0 {
                        return
                }
                if string(block.Header.PrevBlockHash) != string(prevBlockHash) {
                        errorsChan <- fmt.Errorf("invalid previous block hash")
                }
        }()
        
        wg.Add(1)
        go func() {
                defer wg.Done()
                if !pohVerifier(block.PoHSequence) {
                        errorsChan <- fmt.Errorf("invalid PoH sequence")
                }
        }()
        
        wg.Add(1)
        go func() {
                defer wg.Done()
                results := ValidateTransactionsConcurrently(block.Transactions, state)
                for _, result := range results {
                        if !result.Valid {
                                errorsChan <- fmt.Errorf("invalid transaction %s: %v", result.TxID, result.Error)
                                return
                        }
                }
        }()
        
        wg.Wait()
        close(errorsChan)
        
        if len(errorsChan) > 0 {
                return <-errorsChan
        }
        
        return nil
}
