package main

import (
        "context"
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "fmt"
        "log"
        "math/big"
        "net/http"
        "os"
        "strconv"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/api"
        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/consensus"
        "rnr-blockchain/pkg/core"
        "rnr-blockchain/pkg/genesis"
        "rnr-blockchain/pkg/logging"
        "rnr-blockchain/pkg/mempool"
        "rnr-blockchain/pkg/metrics"
        "rnr-blockchain/pkg/network"
        "rnr-blockchain/pkg/sync"
        "rnr-blockchain/pkg/utils"
        "rnr-blockchain/pkg/wallet"
)

func main() {
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        fmt.Println("🚀 ROUTE N ROOT (RNR) Blockchain Node")
        fmt.Println("   Layer 1 Blockchain with PoB + PoH")
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        fmt.Println()

        shutdownMgr := utils.NewShutdownManager(10 * time.Second)
        errorRecovery := utils.NewErrorRecovery(3, 1*time.Second)
        resourceLimiter := utils.NewResourceLimiter(1000)

        useJSONLogs := os.Getenv("RNR_JSON_LOGS") == "true"
        logger := logging.NewStructuredLogger(logging.INFO, useJSONLogs)
        logging.SetDefaultLogger(logger)

        prometheusMetrics := metrics.NewPrometheusMetrics()
        blockchainMetrics := metrics.NewBlockchainMetrics(prometheusMetrics)

        var genesisConfig *genesis.GenesisConfig
        genesisFile := os.Getenv("RNR_GENESIS_CONFIG")
        if genesisFile != "" {
                log.Printf("📜 Loading genesis config from: %s", genesisFile)
                var err error
                genesisConfig, err = genesis.LoadGenesisConfig(genesisFile)
                if err != nil {
                        log.Fatalf("❌ Failed to load genesis config: %v", err)
                }
                fmt.Printf("   Chain ID: %s\n", genesisConfig.ChainID)
                fmt.Printf("   Network: %s\n", genesisConfig.NetworkName)
                fmt.Printf("   Genesis Validators: %d\n", len(genesisConfig.InitialValidators))
        }

        var validatorWallet *wallet.Wallet
        walletFile := os.Getenv("RNR_WALLET_FILE")
        walletPassword := os.Getenv("RNR_WALLET_PASSWORD")

        if walletFile != "" && walletPassword != "" {
                log.Printf("🔐 Loading wallet from: %s", walletFile)
                var err error
                validatorWallet, err = wallet.LoadWalletFromFile(walletPassword, walletFile)
                if err != nil {
                        log.Fatalf("❌ Failed to load wallet: %v", err)
                }
                fmt.Printf("   Address: %s\n", validatorWallet.Address)
        } else {
                log.Printf("⚠️  No wallet file specified, generating new wallet (DEV MODE)")
                mnemonic, err := wallet.GenerateMnemonic()
                if err != nil {
                        log.Fatalf("❌ Failed to generate mnemonic: %v", err)
                }

                validatorWallet, err = wallet.NewWalletFromMnemonic(mnemonic)
                if err != nil {
                        log.Fatalf("❌ Failed to create validator wallet: %v", err)
                }
                fmt.Printf("   ⚠️  DEV MODE: New wallet generated: %s\n", validatorWallet.Address)
        }

        validatorPrivKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
        if err != nil {
                log.Fatalf("❌ Failed to generate validator key: %v", err)
        }

        dbPath := fmt.Sprintf("./data/rnr-db-%s", validatorWallet.Address[:12])
        os.MkdirAll("./data", 0755)

        db, err := leveldb.OpenFile(dbPath, nil)
        if err != nil {
                log.Fatalf("❌ Failed to open database: %v", err)
        }
        
        shutdownMgr.RegisterShutdownHook("database", func() error {
                log.Printf("💾 Closing database...")
                return db.Close()
        })

        chain, err := blockchain.NewBlockchain(db)
        if err != nil {
                log.Fatalf("❌ Failed to create blockchain: %v", err)
        }

        state, err := blockchain.NewState(db)
        if err != nil {
                log.Fatalf("❌ Failed to create state: %v", err)
        }

        mp := blockchain.NewMempool()
        poh := consensus.NewProofOfHistory()

        pubKeyBytes, _ := core.EncodePublicKey(&validatorPrivKey.PublicKey)
        validatorID := validatorWallet.Address
        state.CreateGenesisValidator(validatorID, pubKeyBytes)

        validatorService, err := consensus.NewValidatorService(
                validatorID,
                validatorPrivKey,
                chain,
                state,
                mp,
                poh,
        )
        if err != nil {
                log.Fatalf("❌ Failed to create validator service: %v", err)
        }

        ctx := context.Background()

        p2pPort := 6000
        if portStr := os.Getenv("RNR_P2P_PORT"); portStr != "" {
                if port, err := strconv.Atoi(portStr); err == nil {
                        p2pPort = port
                }
        }

        p2pNode, err := network.NewP2PNetwork(p2pPort)
        if err != nil {
                log.Printf("⚠️  P2P initialization failed: %v", err)
        }

        var discovery *network.PeerDiscovery
        if p2pNode != nil {
                discovery, err = network.NewPeerDiscovery(ctx, p2pNode.Host)
                if err != nil {
                        log.Printf("⚠️  Peer discovery failed: %v", err)
                }
        }

        // Start PoB Test Server for speed test measurements (Whitepaper Bab 3.1.2)
        pobPort := 8080
        if portStr := os.Getenv("RNR_POB_PORT"); portStr != "" {
                if port, err := strconv.Atoi(portStr); err == nil {
                        pobPort = port
                }
        }
        
        pobServer := consensus.NewPoBTestServer(pobPort)
        if err := pobServer.Start(); err != nil {
                log.Fatalf("❌ %v", err) // Fatal: PoB test server is critical for mainnet
        }
        
        // Register PoB server shutdown with defer
        defer func() {
                log.Printf("🛑 Stopping PoB test server...")
                pobServer.Stop()
        }()

        forkResolver := blockchain.NewForkResolver(chain, db)
        syncManager := sync.NewSyncManager(chain)
        mempoolSync := mempool.NewMempoolSync(mp)
        validatorRegistry := consensus.NewValidatorRegistry(state)
        checkpointMgr := consensus.NewCheckpointManager(db, 100)
        partitionDetector := consensus.NewPartitionDetector(1)
        slashingMgr := consensus.NewSlashingManager(db, validatorRegistry)
        gossipProtocol := network.NewGossipProtocol(3, 5)

        // State Pruner: Database optimization and cleanup
        retentionBlocks := uint64(1000) // Keep last 1000 blocks AFTER finalized checkpoint
        if retStr := os.Getenv("RNR_RETENTION_BLOCKS"); retStr != "" {
                if ret, err := strconv.ParseUint(retStr, 10, 64); err == nil {
                        retentionBlocks = ret
                }
        }
        statePruner := blockchain.NewStatePruner(db, chain, checkpointMgr, retentionBlocks, 24*time.Hour)

        if p2pNode != nil {
                p2pNode.SetBlockHandler(func(block *core.Block) error {
                        if err := forkResolver.HandleCompetingBlock(block); err != nil {
                                return err
                        }
                        return nil
                })

                p2pNode.SetVoteHandler(func(blockHash []byte, validatorID string, signature []byte) error {
                        log.Printf("📥 Received vote from %s for block %x", validatorID[:12], blockHash[:8])
                        return nil
                })

                p2pNode.SetTransactionHandler(func(tx *core.Transaction) error {
                        if err := mp.AddTransaction(tx); err != nil {
                                log.Printf("⚠️  Failed to add transaction to mempool: %v", err)
                                return err
                        }
                        log.Printf("📥 Added transaction %s to mempool", tx.ID[:12])
                        return nil
                })

                // Set transaction lookup handler for P2P requests
                p2pNode.SetTxLookupHandler(func(txID string) (*core.Transaction, error) {
                        tx := mp.GetTransaction(txID)
                        if tx == nil {
                                return nil, fmt.Errorf("transaction not found: %s", txID[:12])
                        }
                        return tx, nil
                })

                // Wire up mempool sync with P2P network
                mempoolSync.SetBroadcastFunc(func(tx *core.Transaction) error {
                        return p2pNode.BroadcastTransaction(tx)
                })

                mempoolSync.SetRequestTxFunc(func(txID string) (*core.Transaction, error) {
                        peers := p2pNode.GetPeers()
                        if len(peers) == 0 {
                                return nil, fmt.Errorf("no peers available")
                        }
                        // Request from first available peer
                        return p2pNode.RequestTransaction(peers[0], txID)
                })

                // Start mempool sync goroutine
                utils.SafeGoroutine("mempool-sync", func() {
                        mempoolSync.SyncWithPeers(
                                func() []string { return p2pNode.GetPeers() },
                                func(peerID string, req *mempool.TxRequest) (*mempool.TxResponse, error) {
                                        // Request transactions from peer
                                        txs := make([]*core.Transaction, 0)
                                        for _, txID := range req.TxIDs {
                                                tx, err := p2pNode.RequestTransaction(peerID, txID)
                                                if err != nil {
                                                        log.Printf("⚠️  Failed to request tx %s: %v", txID[:12], err)
                                                        continue
                                                }
                                                txs = append(txs, tx)
                                        }
                                        return &mempool.TxResponse{Transactions: txs}, nil
                                },
                        )
                })

                log.Printf("✅ Mempool sync enabled with P2P network")
        }

        utils.SafeGoroutine("validator-registry", func() {
                ticker := time.NewTicker(30 * time.Second)
                defer ticker.Stop()
                for {
                        select {
                        case <-ticker.C:
                                validatorRegistry.ActivatePendingValidators()
                                validatorRegistry.ProcessExits()
                                partitionDetector.CheckPartitionStatus()
                        case <-shutdownMgr.Context().Done():
                                log.Printf("🛑 Validator registry goroutine stopped")
                                return
                        }
                }
        })

        utils.SafeGoroutine("cleanup", func() {
                ticker := time.NewTicker(1 * time.Minute)
                defer ticker.Stop()
                for {
                        select {
                        case <-ticker.C:
                                slashingMgr.CleanupOldTracking(24 * time.Hour)
                                checkpointMgr.CleanupOldCheckpoints(10)
                                mempoolSync.CleanupOldKnownTxs(1 * time.Hour)
                                
                                if cleared := slashingMgr.CheckAndClearExpiredSuspensions(); cleared > 0 {
                                        log.Printf("🔄 Auto-recovery: %d validators released from suspension", cleared)
                                }
                        case <-shutdownMgr.Context().Done():
                                log.Printf("🛑 Cleanup goroutine stopped")
                                return
                        }
                }
        })

        // State Pruning: Run every hour
        utils.SafeGoroutine("state-pruning", func() {
                ticker := time.NewTicker(1 * time.Hour)
                defer ticker.Stop()
                for {
                        select {
                        case <-ticker.C:
                                if err := statePruner.PerformMaintenance(); err != nil {
                                        log.Printf("⚠️  State pruning failed: %v", err)
                                }
                        case <-shutdownMgr.Context().Done():
                                log.Printf("🛑 State pruning goroutine stopped")
                                return
                        }
                }
        })

        // JSON-RPC/REST API Server (port 5000)
        apiPort := 5000
        if portStr := os.Getenv("RNR_API_PORT"); portStr != "" {
                if port, err := strconv.Atoi(portStr); err == nil {
                        apiPort = port
                }
        }

        apiServer := api.NewAPIServer(chain, state, mp, apiPort)
        utils.SafeGoroutine("api-server", func() {
                if err := apiServer.Start(); err != nil && err != http.ErrServerClosed {
                        log.Printf("⚠️  API server error: %v", err)
                }
        })

        // Register API server shutdown
        defer func() {
                log.Printf("🛑 Stopping API server...")
                apiServer.Stop()
        }()

        _ = syncManager
        _ = gossipProtocol
        _ = discovery

        genesisAccount := &core.Account{
                Address: validatorWallet.Address,
                Balance: big.NewInt(0),
                Nonce:   0,
        }
        state.UpdateAccount(genesisAccount)

        fmt.Println("✅ Node Initialized Successfully!")
        fmt.Printf("   📋 Validator ID: %s\n", validatorID[:16])
        fmt.Printf("   💾 Database: %s\n", dbPath)
        fmt.Printf("   📦 Block Height: %d\n", chain.GetLatestBlock().Header.Height)
        fmt.Printf("   💰 Genesis Balance: %s RNR (coins earned via block validation)\n", genesisAccount.Balance.String())
        fmt.Printf("   🔗 Active Validators: %d\n", len(state.GetActiveValidators()))
        fmt.Printf("   ⏰ PoH Sequence: %x...\n", poh.GetSequence()[:8])
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
        fmt.Println()

        blockTicker := time.NewTicker(core.BlockTime)
        defer blockTicker.Stop()

        pohTicker := time.NewTicker(5 * time.Second)
        defer pohTicker.Stop()

        metricsPort := 9090
        if portStr := os.Getenv("RNR_METRICS_PORT"); portStr != "" {
                if port, err := strconv.Atoi(portStr); err == nil {
                        metricsPort = port
                }
        }

        http.Handle("/metrics", prometheusMetrics.Handler())
        utils.SafeGoroutine("metrics-server", func() {
                addr := fmt.Sprintf(":%d", metricsPort)
                log.Printf("📊 Metrics server listening on %s", addr)
                if err := http.ListenAndServe(addr, nil); err != nil {
                        log.Printf("⚠️  Metrics server error: %v", err)
                }
        })

        utils.SafeGoroutine("metrics-updater", func() {
                ticker := time.NewTicker(10 * time.Second)
                defer ticker.Stop()
                for {
                        select {
                        case <-ticker.C:
                                blockchainMetrics.BlockHeight.Set(float64(chain.GetLatestBlock().Header.Height))
                                blockchainMetrics.ValidatorCount.Set(float64(len(state.GetActiveValidators())))
                                blockchainMetrics.MempoolSize.Set(float64(len(mp.GetTransactions(100))))
                                if p2pNode != nil {
                                        blockchainMetrics.PeerCount.Set(float64(p2pNode.GetPeerCount()))
                                }
                        case <-shutdownMgr.Context().Done():
                                return
                        }
                }
        })

        fmt.Println("💡 Node is running. Consensus active...")
        fmt.Println("   - Block production every 30 seconds")
        fmt.Println("   - PoH updates every 5 seconds")
        fmt.Printf("   - Metrics endpoint: http://localhost:%d/metrics\n", metricsPort)
        fmt.Println("   - Press Ctrl+C to stop")
        fmt.Println()

        utils.SafeGoroutine("poh-updates", func() {
                for {
                        select {
                        case <-pohTicker.C:
                                poh.Update(poh.GetSequence())
                        case <-shutdownMgr.Context().Done():
                                log.Printf("🛑 PoH update goroutine stopped")
                                return
                        }
                }
        })

        dbCircuitBreaker := utils.NewCircuitBreaker("database", 5, 30*time.Second)
        
        utils.SafeGoroutine("block-production", func() {
                blockCount := 0
                for {
                        select {
                        case <-blockTicker.C:
                                blockCount++
                                
                                err := errorRecovery.RetryWithBackoff(func() error {
                                        latestBlock := chain.GetLatestBlock()
                                        nextHeight := latestBlock.Header.Height + 1

                                        isProposer, proposerID, err := validatorService.IsProposer(nextHeight)
                                        if err != nil {
                                                return fmt.Errorf("error checking proposer: %w", err)
                                        }

                                        fmt.Printf("\n⏰ Block Cycle #%d | Height: %d\n", blockCount, nextHeight)
                                        fmt.Printf("   Proposer: %s%s\n", proposerID[:12], func() string {
                                                if isProposer {
                                                        return " (ME)"
                                                }
                                                return ""
                                        }())

                                        if isProposer {
                                                startTime := time.Now()
                                                var block *core.Block
                                                proposeErr := dbCircuitBreaker.Call(func() error {
                                                        var err error
                                                        block, err = validatorService.ProposeBlock()
                                                        return err
                                                })
                                                
                                                if proposeErr != nil {
                                                        return fmt.Errorf("failed to propose block: %w", proposeErr)
                                                }

                                                blockchainMetrics.BlockProductionTime.ObserveDuration(startTime)
                                                blockchainMetrics.TotalTransactions.Add(int64(len(block.Transactions)))

                                                fmt.Printf("   📝 Proposed block #%d with %d txs\n", block.Header.Height, len(block.Transactions))
                                                
                                                logger.InfoWithFields("Block proposed", map[string]interface{}{
                                                        "height":   block.Header.Height,
                                                        "txs":      len(block.Transactions),
                                                        "proposer": validatorID[:12],
                                                })

                                                // SECURITY: Broadcast block to network
                                                if p2pNode != nil {
                                                        if err := p2pNode.BroadcastBlock(block); err != nil {
                                                                log.Printf("⚠️  Failed to broadcast block: %v", err)
                                                        } else {
                                                                fmt.Printf("   📡 Block broadcast to network\n")
                                                        }

                                                        // SECURITY: Broadcast VRF proof to network
                                                        if err := p2pNode.BroadcastVRFProof(block); err != nil {
                                                                log.Printf("⚠️  Failed to broadcast VRF proof: %v", err)
                                                        } else {
                                                                fmt.Printf("   🔐 VRF proof broadcast to network\n")
                                                        }
                                                }

                                                if err := validatorService.VoteOnBlock(block); err != nil {
                                                        log.Printf("❌ Failed to vote: %v", err)
                                                } else {
                                                        fmt.Printf("   ✅ Voted on own block\n")
                                                        blockchainMetrics.FinalizedBlocks.Inc()
                                                }
                                        } else {
                                                fmt.Printf("   ⏳ Waiting for proposer's block...\n")
                                        }
                                        
                                        return nil
                                }, "block-production")
                                
                                if err != nil {
                                        log.Printf("⚠️  Block production error: %v", err)
                                }
                                
                        case <-shutdownMgr.Context().Done():
                                log.Printf("🛑 Block production goroutine stopped")
                                return
                        }
                }
        })

        shutdownMgr.RegisterShutdownHook("final-state", func() error {
                finalBlock := chain.GetLatestBlock()
                fmt.Printf("\n   📊 Final Block Height: %d\n", finalBlock.Header.Height)
                fmt.Printf("   👥 Total Validators: %d\n", len(state.GetActiveValidators()))
                fmt.Printf("   🔧 Active Goroutines: %d\n", resourceLimiter.GetActiveCount())
                return nil
        })

        _ = errorRecovery
        _ = resourceLimiter
        _ = genesisConfig
        _ = logger
        _ = blockchainMetrics

        <-shutdownMgr.Context().Done()
        
        fmt.Println("\n✅ Shutdown complete. Goodbye!")
        fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}
