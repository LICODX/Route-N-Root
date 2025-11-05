package blockchain

import (
        "sync"
        "rnr-blockchain/pkg/core"
)

type Mempool struct {
        transactions map[string]*core.Transaction
        priorityTx   []*core.Transaction
        mu           sync.RWMutex
}

func NewMempool() *Mempool {
        return &Mempool{
                transactions: make(map[string]*core.Transaction),
                priorityTx:   make([]*core.Transaction, 0),
        }
}

func (m *Mempool) AddTransaction(tx *core.Transaction) error {
        m.mu.Lock()
        defer m.mu.Unlock()

        if _, ok := m.transactions[tx.ID]; ok {
                return nil
        }

        m.transactions[tx.ID] = tx
        m.priorityTx = append(m.priorityTx, tx)
        return nil
}

func (m *Mempool) GetTransactions(count int) []*core.Transaction {
        m.mu.RLock()
        defer m.mu.RUnlock()

        if count > len(m.priorityTx) {
                count = len(m.priorityTx)
        }

        return m.priorityTx[:count]
}

func (m *Mempool) RemoveTransaction(txID string) {
        m.mu.Lock()
        defer m.mu.Unlock()

        delete(m.transactions, txID)

        for i, tx := range m.priorityTx {
                if tx.ID == txID {
                        m.priorityTx = append(m.priorityTx[:i], m.priorityTx[i+1:]...)
                        break
                }
        }
}

func (m *Mempool) Contains(txID string) bool {
        m.mu.RLock()
        defer m.mu.RUnlock()
        _, ok := m.transactions[txID]
        return ok
}

func (m *Mempool) Size() int {
        m.mu.RLock()
        defer m.mu.RUnlock()
        return len(m.transactions)
}

func (m *Mempool) GetTransaction(txID string) *core.Transaction {
        m.mu.RLock()
        defer m.mu.RUnlock()
        return m.transactions[txID]
}

func (m *Mempool) GetPendingTransactions() []*core.Transaction {
        m.mu.RLock()
        defer m.mu.RUnlock()
        txs := make([]*core.Transaction, 0, len(m.priorityTx))
        return append(txs, m.priorityTx...)
}

// GetTransactionsBySize returns transactions from mempool up to maxSizeBytes
// This implements the whitepaper's Dynamic Block Capacity formula
func (m *Mempool) GetTransactionsBySize(maxSizeBytes int) []*core.Transaction {
        m.mu.RLock()
        defer m.mu.RUnlock()

        selected := make([]*core.Transaction, 0)
        totalSize := 0

        for _, tx := range m.priorityTx {
                // Calculate transaction size (approximate serialized size)
                txSize := m.estimateTransactionSize(tx)
                
                if totalSize+txSize > maxSizeBytes {
                        // Reached capacity limit
                        break
                }

                selected = append(selected, tx)
                totalSize += txSize
        }

        return selected
}

// estimateTransactionSize estimates the serialized size of a transaction in bytes
func (m *Mempool) estimateTransactionSize(tx *core.Transaction) int {
        // Rough estimate based on transaction structure:
        // ID (64 bytes) + From (42 bytes) + To (42 bytes) + Amount (32 bytes) +
        // Timestamp (8 bytes) + Nonce (8 bytes) + Fee (32 bytes) +
        // Signature (64 bytes) + Data (variable)
        baseSize := 64 + 42 + 42 + 32 + 8 + 8 + 32 + 64
        dataSize := len(tx.Data)
        
        return baseSize + dataSize
}
