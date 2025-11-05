package api

import (
        "crypto/ecdsa"
        "crypto/elliptic"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "math/big"
        "net/http"
        "strconv"
        "strings"
        "time"

        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
        "rnr-blockchain/pkg/wallet"
)

// APIServer provides JSON-RPC/REST endpoints for external applications
type APIServer struct {
        blockchain *blockchain.Blockchain
        state      *blockchain.State
        mempool    *blockchain.Mempool
        port       int
        server     *http.Server
}

// Response types
type BalanceResponse struct {
        Address     string    `json:"address"`
        Balance     string    `json:"balance"`
        Nonce       uint64    `json:"nonce"`
        LastUpdated time.Time `json:"last_updated"`
}

type TransactionResponse struct {
        ID        string    `json:"id"`
        From      string    `json:"from"`
        To        string    `json:"to"`
        Amount    string    `json:"amount"`
        Fee       string    `json:"fee"`
        Nonce     uint64    `json:"nonce"`
        Timestamp time.Time `json:"timestamp"`
        BlockHash string    `json:"block_hash,omitempty"`
        Status    string    `json:"status"`
}

type BlockResponse struct {
        Height       uint64                 `json:"height"`
        Hash         string                 `json:"hash"`
        PrevHash     string                 `json:"prev_hash"`
        MerkleRoot   string                 `json:"merkle_root"`
        Timestamp    time.Time              `json:"timestamp"`
        Proposer     string                 `json:"proposer"`
        Transactions []TransactionResponse  `json:"transactions"`
        TxCount      int                    `json:"tx_count"`
}

type BlockchainInfoResponse struct {
        ChainID         string `json:"chain_id"`
        NetworkName     string `json:"network_name"`
        LatestHeight    uint64 `json:"latest_height"`
        LatestBlockHash string `json:"latest_block_hash"`
        TotalTx         uint64 `json:"total_tx"`
        ActiveValidators int   `json:"active_validators"`
        MempoolSize     int    `json:"mempool_size"`
        Timestamp       time.Time `json:"timestamp"`
}

type MempoolResponse struct {
        PendingTx    int                    `json:"pending_tx"`
        TotalSize    int                    `json:"total_size_bytes"`
        Transactions []TransactionResponse  `json:"transactions"`
}

type SubmitTxRequest struct {
        From      string `json:"from"`
        To        string `json:"to"`
        Amount    string `json:"amount"`
        Fee       string `json:"fee"`
        Nonce     uint64 `json:"nonce"`
        Timestamp int64  `json:"timestamp"` // Unix timestamp (seconds) - must match signed payload
        Signature string `json:"signature"`
        PublicKey string `json:"public_key"` // SECURITY: Required for signature verification
}

type SubmitTxResponse struct {
        Success bool   `json:"success"`
        TxHash  string `json:"tx_hash,omitempty"`
        Error   string `json:"error,omitempty"`
        Message string `json:"message,omitempty"`
}

type ErrorResponse struct {
        Error   string `json:"error"`
        Code    int    `json:"code"`
        Message string `json:"message,omitempty"`
}

// NewAPIServer creates a new API server
func NewAPIServer(blockchain *blockchain.Blockchain, state *blockchain.State, mempool *blockchain.Mempool, port int) *APIServer {
        return &APIServer{
                blockchain: blockchain,
                state:      state,
                mempool:    mempool,
                port:       port,
        }
}

// Start begins serving API requests
func (s *APIServer) Start() error {
        mux := http.NewServeMux()

        // Register endpoints
        mux.HandleFunc("/api/balance/", s.handleBalance)
        mux.HandleFunc("/api/transactions/", s.handleTransactions)
        mux.HandleFunc("/api/tx/", s.handleTxStatus)
        mux.HandleFunc("/api/submit", s.handleSubmitTx)
        mux.HandleFunc("/api/blocks/", s.handleBlock)
        mux.HandleFunc("/api/info", s.handleBlockchainInfo)
        mux.HandleFunc("/api/mempool", s.handleMempool)
        mux.HandleFunc("/health", s.handleHealth)

        // CORS middleware
        handler := corsMiddleware(mux)

        s.server = &http.Server{
                Addr:         fmt.Sprintf(":%d", s.port),
                Handler:      handler,
                ReadTimeout:  15 * time.Second,
                WriteTimeout: 15 * time.Second,
                IdleTimeout:  60 * time.Second,
        }

        log.Printf("🌐 API Server listening on http://0.0.0.0:%d", s.port)
        log.Printf("   Endpoints:")
        log.Printf("   - GET  /api/balance/:address")
        log.Printf("   - GET  /api/transactions/:address")
        log.Printf("   - GET  /api/tx/:hash")
        log.Printf("   - POST /api/submit")
        log.Printf("   - GET  /api/blocks/:height")
        log.Printf("   - GET  /api/info")
        log.Printf("   - GET  /api/mempool")
        log.Printf("   - GET  /health")

        return s.server.ListenAndServe()
}

// Stop gracefully shuts down the API server
func (s *APIServer) Stop() error {
        if s.server != nil {
                log.Println("🛑 Shutting down API server...")
                return s.server.Close()
        }
        return nil
}

// handleBalance returns account balance and nonce
func (s *APIServer) handleBalance(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        address := strings.TrimPrefix(r.URL.Path, "/api/balance/")
        if address == "" {
                s.writeError(w, "Address required", http.StatusBadRequest)
                return
        }

        // Validate RNR address format
        if !strings.HasPrefix(address, "rnr") || len(address) < 10 {
                s.writeError(w, "Invalid RNR address format", http.StatusBadRequest)
                return
        }

        account, err := s.state.GetAccount(address)
        if err != nil {
                s.writeError(w, "Failed to get account", http.StatusInternalServerError)
                return
        }

        response := BalanceResponse{
                Address:     address,
                Balance:     account.Balance.String(),
                Nonce:       account.Nonce,
                LastUpdated: time.Now(),
        }

        s.writeJSON(w, response, http.StatusOK)
}

// handleTransactions returns transaction history for an address
func (s *APIServer) handleTransactions(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        address := strings.TrimPrefix(r.URL.Path, "/api/transactions/")
        if address == "" {
                s.writeError(w, "Address required", http.StatusBadRequest)
                return
        }

        // Get limit from query params
        limitStr := r.URL.Query().Get("limit")
        limit := 50 // default
        if limitStr != "" {
                if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
                        limit = l
                }
        }

        txs := s.blockchain.GetTransactionHistory(address, limit)
        response := make([]TransactionResponse, 0, len(txs))

        for _, tx := range txs {
                status := "confirmed"
                blockHash := ""
                
                // Check if in mempool (pending)
                if s.mempool.GetTransaction(tx.ID) != nil {
                        status = "pending"
                }

                response = append(response, TransactionResponse{
                        ID:        tx.ID,
                        From:      tx.From,
                        To:        tx.To,
                        Amount:    tx.Amount.String(),
                        Fee:       tx.Fee.String(),
                        Nonce:     tx.Nonce,
                        Timestamp: tx.Timestamp,
                        BlockHash: blockHash,
                        Status:    status,
                })
        }

        s.writeJSON(w, response, http.StatusOK)
}

// handleTxStatus returns status of a specific transaction
func (s *APIServer) handleTxStatus(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        txHash := strings.TrimPrefix(r.URL.Path, "/api/tx/")
        if txHash == "" {
                s.writeError(w, "Transaction hash required", http.StatusBadRequest)
                return
        }

        // Check mempool first (pending)
        if tx := s.mempool.GetTransaction(txHash); tx != nil {
                response := TransactionResponse{
                        ID:        tx.ID,
                        From:      tx.From,
                        To:        tx.To,
                        Amount:    tx.Amount.String(),
                        Fee:       tx.Fee.String(),
                        Nonce:     tx.Nonce,
                        Timestamp: tx.Timestamp,
                        Status:    "pending",
                }
                s.writeJSON(w, response, http.StatusOK)
                return
        }

        // Check blockchain (confirmed)
        tx := s.blockchain.GetTransactionByID(txHash)
        if tx == nil {
                s.writeError(w, "Transaction not found", http.StatusNotFound)
                return
        }

        response := TransactionResponse{
                ID:        tx.ID,
                From:      tx.From,
                To:        tx.To,
                Amount:    tx.Amount.String(),
                Fee:       tx.Fee.String(),
                Nonce:     tx.Nonce,
                Timestamp: tx.Timestamp,
                Status:    "confirmed",
        }

        s.writeJSON(w, response, http.StatusOK)
}

// handleSubmitTx accepts transaction submissions
func (s *APIServer) handleSubmitTx(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        body, err := io.ReadAll(r.Body)
        if err != nil {
                s.writeError(w, "Failed to read request body", http.StatusBadRequest)
                return
        }
        defer r.Body.Close()

        var req SubmitTxRequest
        if err := json.Unmarshal(body, &req); err != nil {
                s.writeError(w, "Invalid JSON format", http.StatusBadRequest)
                return
        }

        // Validate required fields
        if req.From == "" || req.To == "" || req.Amount == "" || req.Signature == "" || req.PublicKey == "" {
                s.writeError(w, "Missing required fields (from, to, amount, signature, public_key)", http.StatusBadRequest)
                return
        }

        // Parse public key from hex
        pubKeyBytes, err := hex.DecodeString(req.PublicKey)
        if err != nil {
                s.writeError(w, "Invalid public key format (must be hex)", http.StatusBadRequest)
                return
        }

        // Decode public key to ECDSA format (64 bytes: 32 for X, 32 for Y)
        if len(pubKeyBytes) != 64 {
                s.writeError(w, "Invalid public key length (must be 64 bytes)", http.StatusBadRequest)
                return
        }

        x := new(big.Int).SetBytes(pubKeyBytes[:32])
        y := new(big.Int).SetBytes(pubKeyBytes[32:])
        publicKey := &ecdsa.PublicKey{
                Curve: elliptic.P256(),
                X:     x,
                Y:     y,
        }

        // SECURITY: Verify that address matches the public key
        derivedAddress, err := wallet.GenerateAddress(*publicKey)
        if err != nil {
                s.writeError(w, "Failed to derive address from public key", http.StatusInternalServerError)
                return
        }

        if derivedAddress != req.From {
                s.writeError(w, "Address does not match public key", http.StatusForbidden)
                return
        }

        // Validate timestamp (must be recent, within 5 minutes)
        clientTime := time.Unix(req.Timestamp, 0)
        if time.Since(clientTime).Abs() > 5*time.Minute {
                s.writeError(w, "Timestamp too far from server time (must be within 5 minutes)", http.StatusBadRequest)
                return
        }

        // Parse signature from hex string to bytes
        sigBytes, err := hex.DecodeString(req.Signature)
        if err != nil {
                s.writeError(w, "Invalid signature format (must be hex)", http.StatusBadRequest)
                return
        }

        // SECURITY: Enforce 64-byte signature (32 bytes r + 32 bytes s with padding)
        if len(sigBytes) != 64 {
                s.writeError(w, "Invalid signature length (must be 64 bytes: 32r+32s)", http.StatusBadRequest)
                return
        }

        // Initialize transaction with proper big.Int values
        amount := new(big.Int)
        if _, ok := amount.SetString(req.Amount, 10); !ok {
                s.writeError(w, "Invalid amount format", http.StatusBadRequest)
                return
        }

        fee := new(big.Int)
        if req.Fee != "" {
                if _, ok := fee.SetString(req.Fee, 10); !ok {
                        s.writeError(w, "Invalid fee format", http.StatusBadRequest)
                        return
                }
        } else {
                fee.SetInt64(1000000000000000) // Default 0.001 RNR
        }

        // Create transaction object with client-provided timestamp
        tx := &core.Transaction{
                From:      req.From,
                To:        req.To,
                Amount:    amount,
                Fee:       fee,
                Nonce:     req.Nonce,
                Timestamp: clientTime,
                Signature: sigBytes,
        }

        // Calculate transaction hash
        txHash, err := tx.Hash()
        if err != nil {
                s.writeError(w, "Failed to calculate transaction hash", http.StatusInternalServerError)
                return
        }
        tx.ID = fmt.Sprintf("%x", txHash)

        // SECURITY: Verify signature authenticity
        // Split signature into r and s components (32 bytes each, padded)
        sigR := new(big.Int).SetBytes(sigBytes[:32])
        sigS := new(big.Int).SetBytes(sigBytes[32:64])

        // Verify ECDSA signature
        if !ecdsa.Verify(publicKey, txHash, sigR, sigS) {
                s.writeError(w, "Invalid signature - signature verification failed", http.StatusForbidden)
                log.Printf("🚨 SECURITY: Failed signature verification for address %s", req.From)
                return
        }

        log.Printf("✅ SECURITY: Signature verified for transaction from %s", req.From)

        // Add to mempool
        if err := s.mempool.AddTransaction(tx); err != nil {
                response := SubmitTxResponse{
                        Success: false,
                        Error:   err.Error(),
                        Message: "Transaction rejected by mempool",
                }
                s.writeJSON(w, response, http.StatusBadRequest)
                return
        }

        response := SubmitTxResponse{
                Success: true,
                TxHash:  tx.ID,
                Message: "Transaction submitted successfully",
        }

        log.Printf("📨 API: Transaction submitted - %s", tx.ID)
        s.writeJSON(w, response, http.StatusOK)
}

// handleBlock returns block details by height
func (s *APIServer) handleBlock(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        heightStr := strings.TrimPrefix(r.URL.Path, "/api/blocks/")
        if heightStr == "" || heightStr == "latest" {
                // Return latest block
                block := s.blockchain.GetLatestBlock()
                s.respondBlock(w, block)
                return
        }

        height, err := strconv.ParseUint(heightStr, 10, 64)
        if err != nil {
                s.writeError(w, "Invalid block height", http.StatusBadRequest)
                return
        }

        block, err := s.blockchain.GetBlockByHeight(height)
        if err != nil {
                s.writeError(w, "Block not found", http.StatusNotFound)
                return
        }

        s.respondBlock(w, block)
}

// handleBlockchainInfo returns general blockchain information
func (s *APIServer) handleBlockchainInfo(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        latestBlock := s.blockchain.GetLatestBlock()
        blockHash, _ := latestBlock.Hash()
        
        activeValidators := s.state.GetActiveValidators()
        mempoolTxs := s.mempool.GetTransactions(1000)

        response := BlockchainInfoResponse{
                ChainID:         "rnr-mainnet-1",
                NetworkName:     "RNR Mainnet",
                LatestHeight:    latestBlock.Header.Height,
                LatestBlockHash: fmt.Sprintf("%x", blockHash),
                TotalTx:         s.blockchain.GetTotalTransactionCount(),
                ActiveValidators: len(activeValidators),
                MempoolSize:     len(mempoolTxs),
                Timestamp:       time.Now(),
        }

        s.writeJSON(w, response, http.StatusOK)
}

// handleMempool returns current mempool status
func (s *APIServer) handleMempool(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
                s.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }

        txs := s.mempool.GetTransactions(100) // Limit to 100 for performance
        totalSize := 0
        
        response := MempoolResponse{
                PendingTx:    len(txs),
                TotalSize:    totalSize,
                Transactions: make([]TransactionResponse, 0, len(txs)),
        }

        for _, tx := range txs {
                response.Transactions = append(response.Transactions, TransactionResponse{
                        ID:        tx.ID,
                        From:      tx.From,
                        To:        tx.To,
                        Amount:    tx.Amount.String(),
                        Fee:       tx.Fee.String(),
                        Nonce:     tx.Nonce,
                        Timestamp: tx.Timestamp,
                        Status:    "pending",
                })
                
                // Estimate size (rough calculation)
                totalSize += 250 // average tx size
        }

        response.TotalSize = totalSize

        s.writeJSON(w, response, http.StatusOK)
}

// handleHealth returns server health status
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
        health := map[string]interface{}{
                "status":    "ok",
                "timestamp": time.Now(),
                "uptime":    time.Since(time.Now()).String(),
        }
        s.writeJSON(w, health, http.StatusOK)
}

// Helper: respond with block details
func (s *APIServer) respondBlock(w http.ResponseWriter, block *core.Block) {
        blockHash, _ := block.Hash()
        
        txResponses := make([]TransactionResponse, 0, len(block.Transactions))
        for _, tx := range block.Transactions {
                txResponses = append(txResponses, TransactionResponse{
                        ID:        tx.ID,
                        From:      tx.From,
                        To:        tx.To,
                        Amount:    tx.Amount.String(),
                        Fee:       tx.Fee.String(),
                        Nonce:     tx.Nonce,
                        Timestamp: tx.Timestamp,
                        BlockHash: fmt.Sprintf("%x", blockHash),
                        Status:    "confirmed",
                })
        }

        response := BlockResponse{
                Height:       block.Header.Height,
                Hash:         fmt.Sprintf("%x", blockHash),
                PrevHash:     fmt.Sprintf("%x", block.Header.PrevBlockHash),
                MerkleRoot:   fmt.Sprintf("%x", block.Header.MerkleRoot),
                Timestamp:    block.Header.Timestamp,
                Proposer:     block.ProposerID,
                Transactions: txResponses,
                TxCount:      len(block.Transactions),
        }

        s.writeJSON(w, response, http.StatusOK)
}

// Helper: write JSON response
func (s *APIServer) writeJSON(w http.ResponseWriter, data interface{}, status int) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(status)
        json.NewEncoder(w).Encode(data)
}

// Helper: write error response
func (s *APIServer) writeError(w http.ResponseWriter, message string, code int) {
        response := ErrorResponse{
                Error:   http.StatusText(code),
                Code:    code,
                Message: message,
        }
        s.writeJSON(w, response, code)
}

// CORS middleware
func corsMiddleware(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Access-Control-Allow-Origin", "*")
                w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

                if r.Method == "OPTIONS" {
                        w.WriteHeader(http.StatusOK)
                        return
                }

                next.ServeHTTP(w, r)
        })
}
