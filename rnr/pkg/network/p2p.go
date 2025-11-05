package network

import (
        "context"
        "crypto/rand"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "sync"
        "time"

        "github.com/libp2p/go-libp2p"
        "github.com/libp2p/go-libp2p/core/crypto"
        "github.com/libp2p/go-libp2p/core/host"
        "github.com/libp2p/go-libp2p/core/network"
        "github.com/libp2p/go-libp2p/core/peer"
        "github.com/libp2p/go-libp2p/core/protocol"
        "github.com/multiformats/go-multiaddr"

        "rnr-blockchain/pkg/core"
)

const (
        ProtocolID          = "/rnr/1.0.0"
        BlockProtocol       = "/rnr/block/1.0.0"
        VoteProtocol        = "/rnr/vote/1.0.0"
        TransactionProtocol = "/rnr/tx/1.0.0"
        SpeedTestReqProtocol = "/rnr/pob-speed/req/1.0.0"
        SpeedTestStreamProtocol = "/rnr/pob-speed/stream/1.0.0"
)

type P2PNetwork struct {
        Host            host.Host
        ctx             context.Context
        cancel          context.CancelFunc
        peers           map[peer.ID]bool
        mu              sync.RWMutex
        blockHandler    BlockHandler
        voteHandler     VoteHandler
        txHandler       TransactionHandler
        txLookupHandler TxLookupHandler      // Lookup transaction for requests
        authManager     *P2PAuthManager      // SECURITY: Peer authentication
        rateLimiter     *RateLimiter         // SECURITY: Rate limiting per peer
        ipReputation    *IPReputationSystem  // SECURITY: IP reputation tracking (1% missing security)
}

type BlockHandler func(*core.Block) error
type VoteHandler func(blockHash []byte, validatorID string, signature []byte) error
type TransactionHandler func(*core.Transaction) error
type TxLookupHandler func(txID string) (*core.Transaction, error) // Lookup transaction by ID

type Message struct {
        Type    int
        Payload []byte
}

func NewP2PNetwork(port int) (*P2PNetwork, error) {
        ctx, cancel := context.WithCancel(context.Background())

        privKey, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
        if err != nil {
                cancel()
                return nil, fmt.Errorf("failed to generate key pair: %w", err)
        }

        listenAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
        if err != nil {
                cancel()
                return nil, fmt.Errorf("failed to create listen address: %w", err)
        }

        h, err := libp2p.New(
                libp2p.ListenAddrs(listenAddr),
                libp2p.Identity(privKey),
                libp2p.NATPortMap(),
                libp2p.EnableNATService(),
        )
        if err != nil {
                cancel()
                return nil, fmt.Errorf("failed to create libp2p host: %w", err)
        }

        // SECURITY: Initialize all security systems (including 1% missing - IP reputation & Byzantine detection)
        authManager := NewP2PAuthManager()
        rateLimiter := NewRateLimiter(
                60,           // 60 requests per minute
                10,           // burst of 10 requests
                10*1024*1024, // 10 MB/s bandwidth limit
        )
        ipReputation := NewIPReputationSystem()

        p2p := &P2PNetwork{
                Host:         h,
                ctx:          ctx,
                cancel:       cancel,
                peers:        make(map[peer.ID]bool),
                authManager:  authManager,
                rateLimiter:  rateLimiter,
                ipReputation: ipReputation,
        }

        h.SetStreamHandler(protocol.ID(BlockProtocol), p2p.handleBlockStream)
        h.SetStreamHandler(protocol.ID(VoteProtocol), p2p.handleVoteStream)
        h.SetStreamHandler(protocol.ID(TransactionProtocol), p2p.handleTransactionStream)

        // SECURITY: Setup authentication protocol
        p2p.SetupAuthProtocol()

        // SECURITY: Start background cleanup routines
        authManager.StartCleanupRoutine()
        rateLimiter.StartCleanupRoutine()
        ipReputation.StartReputationDecay()

        log.Printf("🌐 P2P Network Started")
        log.Printf("   Peer ID: %s", h.ID().String())
        for _, addr := range h.Addrs() {
                log.Printf("   Listening on: %s/p2p/%s", addr, h.ID().String())
        }
        log.Printf("🔒 P2P Security: Authentication, Rate Limiting & IP Reputation enabled")

        return p2p, nil
}

func (p *P2PNetwork) SetBlockHandler(handler BlockHandler) {
        p.blockHandler = handler
}

func (p *P2PNetwork) SetVoteHandler(handler VoteHandler) {
        p.voteHandler = handler
}

func (p *P2PNetwork) SetTransactionHandler(handler TransactionHandler) {
        p.txHandler = handler
}

func (p *P2PNetwork) SetTxLookupHandler(handler TxLookupHandler) {
        p.txLookupHandler = handler
}

// GetPeers returns list of connected peer IDs
func (p *P2PNetwork) GetPeers() []string {
        p.mu.RLock()
        defer p.mu.RUnlock()

        peers := make([]string, 0, len(p.peers))
        for pid := range p.peers {
                peers = append(peers, pid.String())
        }
        return peers
}

// RequestTransaction requests a transaction from a specific peer
func (p *P2PNetwork) RequestTransaction(peerID string, txID string) (*core.Transaction, error) {
        pid, err := peer.Decode(peerID)
        if err != nil {
                return nil, fmt.Errorf("invalid peer ID: %w", err)
        }

        reqData, err := json.Marshal(map[string]string{"tx_id": txID})
        if err != nil {
                return nil, fmt.Errorf("failed to marshal request: %w", err)
        }

        msg := &Message{
                Type:    core.MessageTypeTxRequest,
                Payload: reqData,
        }

        stream, err := p.Host.NewStream(p.ctx, pid, protocol.ID(TransactionProtocol))
        if err != nil {
                return nil, fmt.Errorf("failed to create stream: %w", err)
        }
        defer stream.Close()

        msgData, err := json.Marshal(msg)
        if err != nil {
                return nil, fmt.Errorf("failed to marshal message: %w", err)
        }

        if _, err := stream.Write(msgData); err != nil {
                return nil, fmt.Errorf("failed to write request: %w", err)
        }

        // Close write side to signal request complete (prevents deadlock)
        stream.CloseWrite()

        // Read response
        respData, err := io.ReadAll(stream)
        if err != nil {
                return nil, fmt.Errorf("failed to read response: %w", err)
        }

        // Check for error response
        var errResp map[string]string
        if err := json.Unmarshal(respData, &errResp); err == nil {
                if errMsg, hasErr := errResp["error"]; hasErr {
                        return nil, fmt.Errorf("peer error: %s", errMsg)
                }
        }

        // Parse transaction
        var tx core.Transaction
        if err := json.Unmarshal(respData, &tx); err != nil {
                return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
        }

        // Validate non-empty transaction
        if tx.ID == "" {
                return nil, fmt.Errorf("received empty transaction")
        }

        return &tx, nil
}

func (p *P2PNetwork) ConnectToPeer(peerAddr string) error {
        maddr, err := multiaddr.NewMultiaddr(peerAddr)
        if err != nil {
                return fmt.Errorf("invalid peer address: %w", err)
        }

        peerInfo, err := peer.AddrInfoFromP2pAddr(maddr)
        if err != nil {
                return fmt.Errorf("failed to get peer info: %w", err)
        }

        if err := p.Host.Connect(p.ctx, *peerInfo); err != nil {
                return fmt.Errorf("failed to connect to peer: %w", err)
        }

        p.mu.Lock()
        p.peers[peerInfo.ID] = true
        p.mu.Unlock()

        log.Printf("✅ Connected to peer: %s", peerInfo.ID.String())

        // SECURITY: Auto-authenticate newly connected peer via proper protocol
        go func() {
                time.Sleep(500 * time.Millisecond) // Brief delay to ensure connection stable
                if err := p.AuthenticatePeerViaProtocol(peerInfo.ID); err != nil {
                        log.Printf("⚠️  Failed to authenticate peer %s: %v", peerInfo.ID.String()[:8], err)
                }
        }()

        return nil
}

func (p *P2PNetwork) BroadcastBlock(block *core.Block) error {
        blockData, err := json.Marshal(block)
        if err != nil {
                return fmt.Errorf("failed to marshal block: %w", err)
        }

        msg := &Message{
                Type:    core.MessageTypeBlock,
                Payload: blockData,
        }

        return p.broadcast(BlockProtocol, msg)
}

func (p *P2PNetwork) BroadcastVote(blockHash []byte, validatorID string, signature []byte) error {
        voteData := map[string]interface{}{
                "block_hash":   blockHash,
                "validator_id": validatorID,
                "signature":    signature,
        }

        payload, err := json.Marshal(voteData)
        if err != nil {
                return fmt.Errorf("failed to marshal vote: %w", err)
        }

        msg := &Message{
                Type:    core.MessageTypeVote,
                Payload: payload,
        }

        return p.broadcast(VoteProtocol, msg)
}

func (p *P2PNetwork) BroadcastTransaction(tx *core.Transaction) error {
        txData, err := json.Marshal(tx)
        if err != nil {
                return fmt.Errorf("failed to marshal transaction: %w", err)
        }

        msg := &Message{
                Type:    core.MessageTypeTransaction,
                Payload: txData,
        }

        return p.broadcast(TransactionProtocol, msg)
}

func (p *P2PNetwork) broadcast(protocolID string, msg *Message) error {
        p.mu.RLock()
        peerIDs := make([]peer.ID, 0, len(p.peers))
        for peerID := range p.peers {
                peerIDs = append(peerIDs, peerID)
        }
        p.mu.RUnlock()

        msgData, err := json.Marshal(msg)
        if err != nil {
                return fmt.Errorf("failed to marshal message: %w", err)
        }

        for _, peerID := range peerIDs {
                go func(pid peer.ID) {
                        stream, err := p.Host.NewStream(p.ctx, pid, protocol.ID(protocolID))
                        if err != nil {
                                log.Printf("⚠️  Failed to open stream to %s: %v", pid.String(), err)
                                return
                        }
                        defer stream.Close()

                        if _, err := stream.Write(msgData); err != nil {
                                log.Printf("⚠️  Failed to send message to %s: %v", pid.String(), err)
                        }
                }(peerID)
        }

        return nil
}

func (p *P2PNetwork) handleBlockStream(stream network.Stream) {
        defer stream.Close()

        remotePeer := stream.Conn().RemotePeer()

        // SECURITY: Check peer authentication
        if !p.authManager.IsAuthenticated(remotePeer) {
                log.Printf("⚠️  Rejected block from unauthenticated peer %s", remotePeer.String()[:8])
                return
        }

        // SECURITY: Check rate limit
        allowed, err := p.rateLimiter.AllowRequest(remotePeer)
        if !allowed {
                log.Printf("⚠️  Rate limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        data, err := io.ReadAll(stream)
        if err != nil {
                log.Printf("⚠️  Failed to read block stream: %v", err)
                return
        }

        // SECURITY: Check bandwidth limit
        allowed, err = p.rateLimiter.AllowBytes(remotePeer, int64(len(data)))
        if !allowed {
                log.Printf("⚠️  Bandwidth limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        var msg Message
        if err := json.Unmarshal(data, &msg); err != nil {
                log.Printf("⚠️  Failed to unmarshal block message: %v", err)
                return
        }

        var block core.Block
        if err := json.Unmarshal(msg.Payload, &block); err != nil {
                log.Printf("⚠️  Failed to unmarshal block: %v", err)
                return
        }

        if p.blockHandler != nil {
                if err := p.blockHandler(&block); err != nil {
                        log.Printf("⚠️  Block handler error: %v", err)
                }
        }
}

func (p *P2PNetwork) handleVoteStream(stream network.Stream) {
        defer stream.Close()

        remotePeer := stream.Conn().RemotePeer()

        // SECURITY: Check peer authentication
        if !p.authManager.IsAuthenticated(remotePeer) {
                log.Printf("⚠️  Rejected vote from unauthenticated peer %s", remotePeer.String()[:8])
                return
        }

        // SECURITY: Check rate limit
        allowed, err := p.rateLimiter.AllowRequest(remotePeer)
        if !allowed {
                log.Printf("⚠️  Rate limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        data, err := io.ReadAll(stream)
        if err != nil {
                log.Printf("⚠️  Failed to read vote stream: %v", err)
                return
        }

        // SECURITY: Check bandwidth limit
        allowed, err = p.rateLimiter.AllowBytes(remotePeer, int64(len(data)))
        if !allowed {
                log.Printf("⚠️  Bandwidth limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        var msg Message
        if err := json.Unmarshal(data, &msg); err != nil {
                log.Printf("⚠️  Failed to unmarshal vote message: %v", err)
                return
        }

        var voteData map[string]interface{}
        if err := json.Unmarshal(msg.Payload, &voteData); err != nil {
                log.Printf("⚠️  Failed to unmarshal vote data: %v", err)
                return
        }

        blockHash := voteData["block_hash"].([]byte)
        validatorID := voteData["validator_id"].(string)
        signature := voteData["signature"].([]byte)

        if p.voteHandler != nil {
                if err := p.voteHandler(blockHash, validatorID, signature); err != nil {
                        log.Printf("⚠️  Vote handler error: %v", err)
                }
        }
}

func (p *P2PNetwork) handleTransactionStream(stream network.Stream) {
        defer stream.Close()

        remotePeer := stream.Conn().RemotePeer()

        // SECURITY: Check peer authentication
        if !p.authManager.IsAuthenticated(remotePeer) {
                log.Printf("⚠️  Rejected transaction from unauthenticated peer %s", remotePeer.String()[:8])
                return
        }

        // SECURITY: Check rate limit
        allowed, err := p.rateLimiter.AllowRequest(remotePeer)
        if !allowed {
                log.Printf("⚠️  Rate limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        data, err := io.ReadAll(stream)
        if err != nil {
                log.Printf("⚠️  Failed to read transaction stream: %v", err)
                return
        }

        // SECURITY: Check bandwidth limit
        allowed, err = p.rateLimiter.AllowBytes(remotePeer, int64(len(data)))
        if !allowed {
                log.Printf("⚠️  Bandwidth limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        var msg Message
        if err := json.Unmarshal(data, &msg); err != nil {
                log.Printf("⚠️  Failed to unmarshal transaction message: %v", err)
                return
        }

        switch msg.Type {
        case core.MessageTypeTransaction, 1: // Transaction announcement (support both types for compatibility)
                var tx core.Transaction
                if err := json.Unmarshal(msg.Payload, &tx); err != nil {
                        log.Printf("⚠️  Failed to unmarshal transaction: %v", err)
                        return
                }

                if p.txHandler != nil {
                        if err := p.txHandler(&tx); err != nil {
                                log.Printf("⚠️  Transaction handler error: %v", err)
                        }
                }

        case core.MessageTypeTxRequest: // Transaction request
                var req map[string]string
                if err := json.Unmarshal(msg.Payload, &req); err != nil {
                        log.Printf("⚠️  Failed to unmarshal tx request: %v", err)
                        return
                }

                txID := req["tx_id"]
                log.Printf("📥 Received transaction request for %s from peer %s", txID[:12], remotePeer.String()[:8])

                // Lookup transaction and respond
                if p.txLookupHandler != nil {
                        tx, err := p.txLookupHandler(txID)
                        if err != nil {
                                log.Printf("⚠️  Failed to lookup transaction %s: %v", txID[:12], err)
                                // Send error response
                                errResp := map[string]string{"error": "transaction not found"}
                                errData, _ := json.Marshal(errResp)
                                stream.Write(errData)
                                return
                        }

                        // Send raw transaction as response (RequestTransaction expects raw tx JSON)
                        txData, err := json.Marshal(tx)
                        if err != nil {
                                log.Printf("⚠️  Failed to marshal transaction: %v", err)
                                return
                        }

                        if _, err := stream.Write(txData); err != nil {
                                log.Printf("⚠️  Failed to write transaction response: %v", err)
                        } else {
                                log.Printf("📤 Sent transaction %s to peer %s", txID[:12], remotePeer.String()[:8])
                        }
                } else {
                        // No lookup handler configured
                        errResp := map[string]string{"error": "lookup not configured"}
                        errData, _ := json.Marshal(errResp)
                        stream.Write(errData)
                }

        default:
                log.Printf("⚠️  Unknown transaction message type: %d", msg.Type)
        }
}

func (p *P2PNetwork) GetPeerCount() int {
        p.mu.RLock()
        defer p.mu.RUnlock()
        return len(p.peers)
}

func (p *P2PNetwork) Close() error {
        p.cancel()
        return p.Host.Close()
}
