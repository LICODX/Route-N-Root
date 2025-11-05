package mempool

import (
        "encoding/json"
        "fmt"
        "log"
        "sync"
        "time"

        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
)

type MempoolSync struct {
        mempool         *blockchain.Mempool
        knownTxs        map[string]bool
        broadcastFunc   func(*core.Transaction) error
        requestTxFunc   func(string) (*core.Transaction, error)
        mu              sync.RWMutex
        syncInterval    time.Duration
        maxBatchSize    int
}

type TxAnnouncement struct {
        TxID      string    `json:"tx_id"`
        Timestamp time.Time `json:"timestamp"`
}

type TxRequest struct {
        TxIDs []string `json:"tx_ids"`
}

type TxResponse struct {
        Transactions []*core.Transaction `json:"transactions"`
}

func NewMempoolSync(mempool *blockchain.Mempool) *MempoolSync {
        return &MempoolSync{
                mempool:      mempool,
                knownTxs:     make(map[string]bool),
                syncInterval: 5 * time.Second,
                maxBatchSize: 50,
        }
}

func (ms *MempoolSync) SetBroadcastFunc(fn func(*core.Transaction) error) {
        ms.broadcastFunc = fn
}

func (ms *MempoolSync) SetRequestTxFunc(fn func(string) (*core.Transaction, error)) {
        ms.requestTxFunc = fn
}

func (ms *MempoolSync) BroadcastTransaction(tx *core.Transaction) error {
        ms.mu.Lock()
        ms.knownTxs[tx.ID] = true
        ms.mu.Unlock()

        if ms.broadcastFunc != nil {
                if err := ms.broadcastFunc(tx); err != nil {
                        return fmt.Errorf("broadcast failed: %w", err)
                }
        }

        log.Printf("📡 Broadcasted transaction: %s", tx.ID[:12])
        return nil
}

func (ms *MempoolSync) HandleTxAnnouncement(announcement *TxAnnouncement, peerID string) error {
        ms.mu.Lock()
        if ms.knownTxs[announcement.TxID] {
                ms.mu.Unlock()
                return nil
        }
        ms.knownTxs[announcement.TxID] = true
        ms.mu.Unlock()

        if ms.mempool.GetTransaction(announcement.TxID) != nil {
                return nil
        }

        if ms.requestTxFunc != nil {
                tx, err := ms.requestTxFunc(announcement.TxID)
                if err != nil {
                        return fmt.Errorf("failed to request tx: %w", err)
                }

                if err := ms.mempool.AddTransaction(tx); err != nil {
                        return fmt.Errorf("failed to add tx to mempool: %w", err)
                }

                log.Printf("📥 Received transaction from peer %s: %s", peerID[:12], tx.ID[:12])
        }

        return nil
}

func (ms *MempoolSync) HandleTxRequest(req *TxRequest) (*TxResponse, error) {
        transactions := make([]*core.Transaction, 0)

        for _, txID := range req.TxIDs {
                tx := ms.mempool.GetTransaction(txID)
                if tx != nil {
                        transactions = append(transactions, tx)
                }
        }

        return &TxResponse{
                Transactions: transactions,
        }, nil
}

func (ms *MempoolSync) HandleTxResponse(resp *TxResponse) error {
        for _, tx := range resp.Transactions {
                ms.mu.Lock()
                ms.knownTxs[tx.ID] = true
                ms.mu.Unlock()

                if err := ms.mempool.AddTransaction(tx); err != nil {
                        log.Printf("⚠️  Failed to add transaction %s: %v", tx.ID[:12], err)
                        continue
                }
        }

        return nil
}

func (ms *MempoolSync) SyncWithPeers(getPeers func() []string, requestPeer func(string, *TxRequest) (*TxResponse, error)) {
        ticker := time.NewTicker(ms.syncInterval)
        defer ticker.Stop()

        for range ticker.C {
                ms.performSync(getPeers, requestPeer)
        }
}

func (ms *MempoolSync) performSync(getPeers func() []string, requestPeer func(string, *TxRequest) (*TxResponse, error)) {
        peers := getPeers()
        if len(peers) == 0 {
                return
        }

        localTxs := ms.mempool.GetPendingTransactions()
        if len(localTxs) > 0 {
                for _, tx := range localTxs {
                        ms.mu.Lock()
                        if !ms.knownTxs[tx.ID] {
                                ms.knownTxs[tx.ID] = true
                                ms.mu.Unlock()

                                if ms.broadcastFunc != nil {
                                        ms.broadcastFunc(tx)
                                }
                        } else {
                                ms.mu.Unlock()
                        }
                }
        }
}

func (ms *MempoolSync) GetMempoolStats() map[string]interface{} {
        ms.mu.RLock()
        defer ms.mu.RUnlock()

        return map[string]interface{}{
                "known_transactions": len(ms.knownTxs),
                "mempool_size":       ms.mempool.Size(),
        }
}

func (ms *MempoolSync) CleanupOldKnownTxs(maxAge time.Duration) {
        ms.mu.Lock()
        defer ms.mu.Unlock()

        pendingTxs := ms.mempool.GetPendingTransactions()
        pendingMap := make(map[string]bool)
        for _, tx := range pendingTxs {
                pendingMap[tx.ID] = true
        }

        for txID := range ms.knownTxs {
                if !pendingMap[txID] {
                        delete(ms.knownTxs, txID)
                }
        }

        log.Printf("🧹 Cleaned up known transactions: %d remaining", len(ms.knownTxs))
}

func (announcement *TxAnnouncement) Marshal() ([]byte, error) {
        return json.Marshal(announcement)
}

func UnmarshalTxAnnouncement(data []byte) (*TxAnnouncement, error) {
        var announcement TxAnnouncement
        err := json.Unmarshal(data, &announcement)
        return &announcement, err
}

func (req *TxRequest) Marshal() ([]byte, error) {
        return json.Marshal(req)
}

func UnmarshalTxRequest(data []byte) (*TxRequest, error) {
        var req TxRequest
        err := json.Unmarshal(data, &req)
        return &req, err
}

func (resp *TxResponse) Marshal() ([]byte, error) {
        return json.Marshal(resp)
}

func UnmarshalTxResponse(data []byte) (*TxResponse, error) {
        var resp TxResponse
        err := json.Unmarshal(data, &resp)
        return &resp, err
}
