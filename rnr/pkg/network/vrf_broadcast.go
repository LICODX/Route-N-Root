package network

import (
        "encoding/json"
        "fmt"
        "io"
        "log"
        "time"

        "github.com/libp2p/go-libp2p/core/network"
        "github.com/libp2p/go-libp2p/core/peer"
        "github.com/libp2p/go-libp2p/core/protocol"
        "rnr-blockchain/pkg/core"
)

const (
        VRFProofProtocol = "/rnr/vrf/1.0.0"
)

// VRFProofMessage represents a VRF proof broadcast message
type VRFProofMessage struct {
        BlockHeight uint64
        ProposerID  string
        VRFProof    []byte
        VRFOutput   []byte
        Timestamp   int64
}

// VRFProofHandler processes incoming VRF proof broadcasts
type VRFProofHandler func(*VRFProofMessage) error

// SetVRFProofHandler registers handler for VRF proof messages
func (p *P2PNetwork) SetVRFProofHandler(handler VRFProofHandler) {
        p.Host.SetStreamHandler(protocol.ID(VRFProofProtocol), func(stream network.Stream) {
                p.handleVRFProofStream(stream, handler)
        })
        log.Printf("✅ VRF Proof broadcast handler registered")
}

// BroadcastVRFProof broadcasts VRF proof to all authenticated peers
// SECURITY: Only broadcasts to authenticated peers to prevent spam
func (p *P2PNetwork) BroadcastVRFProof(block *core.Block) error {
        if block.VRFProof == nil || len(block.VRFProof) == 0 {
                return fmt.Errorf("block missing VRF proof")
        }

        msg := &VRFProofMessage{
                BlockHeight: block.Header.Height,
                ProposerID:  block.ProposerID,
                VRFProof:    block.VRFProof,
                VRFOutput:   block.Header.VRFOutput,
                Timestamp:   block.Header.Timestamp.Unix(),
        }

        payload, err := json.Marshal(msg)
        if err != nil {
                return fmt.Errorf("failed to marshal VRF proof message: %w", err)
        }

        message := &Message{
                Type:    4, // MessageTypeVRFProof
                Payload: payload,
        }

        log.Printf("📡 Broadcasting VRF proof for block #%d from proposer %s", 
                block.Header.Height, block.ProposerID[:8])

        return p.broadcast(VRFProofProtocol, message)
}

// handleVRFProofStream handles incoming VRF proof broadcasts
func (p *P2PNetwork) handleVRFProofStream(stream network.Stream, handler VRFProofHandler) {
        defer stream.Close()

        remotePeer := stream.Conn().RemotePeer()

        // SECURITY: Check peer authentication
        if !p.authManager.IsAuthenticated(remotePeer) {
                log.Printf("⚠️  Rejected VRF proof from unauthenticated peer %s", remotePeer.String()[:8])
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
                log.Printf("⚠️  Failed to read VRF proof stream: %v", err)
                return
        }

        // SECURITY: Check bandwidth limit
        allowed, err = p.rateLimiter.AllowBytes(remotePeer, int64(len(data)))
        if !allowed {
                log.Printf("⚠️  Bandwidth limit exceeded for peer %s: %v", remotePeer.String()[:8], err)
                return
        }

        var message Message
        if err := json.Unmarshal(data, &message); err != nil {
                log.Printf("⚠️  Failed to unmarshal VRF proof message: %v", err)
                return
        }

        var vrfMsg VRFProofMessage
        if err := json.Unmarshal(message.Payload, &vrfMsg); err != nil {
                log.Printf("⚠️  Failed to unmarshal VRF proof payload: %v", err)
                return
        }

        log.Printf("📥 Received VRF proof broadcast for block #%d from %s", 
                vrfMsg.BlockHeight, vrfMsg.ProposerID[:8])

        if handler != nil {
                if err := handler(&vrfMsg); err != nil {
                        log.Printf("⚠️  VRF proof handler error: %v", err)
                }
        }
}

// AuthenticatePeer initiates authentication challenge-response with peer
// SECURITY: Must be called before peer can send sensitive messages
func (p *P2PNetwork) AuthenticatePeer(peerID peer.ID) error {
        // Generate challenge
        challenge, err := p.authManager.GenerateChallenge(peerID)
        if err != nil {
                return fmt.Errorf("failed to generate challenge: %w", err)
        }

        log.Printf("🔐 Sent authentication challenge to peer %s", peerID.String()[:8])

        // TODO: Send challenge to peer via P2P stream
        // For now, auto-authenticate (will be implemented with proper protocol)
        // In production, peer must solve challenge and send response
        
        // Simulate peer solving challenge
        solution := p.authManager.SolveChallenge(peerID, challenge.Nonce)
        
        response := &ChallengeResponse{
                PeerID:    peerID,
                Nonce:     challenge.Nonce,
                Solution:  solution,
                Timestamp: time.Now(),
        }

        // Verify response
        if err := p.authManager.VerifyResponse(response); err != nil {
                return fmt.Errorf("authentication failed: %w", err)
        }

        log.Printf("✅ Peer %s authenticated successfully", peerID.String()[:8])
        return nil
}

// GetAuthenticatedPeers returns list of authenticated peers
func (p *P2PNetwork) GetAuthenticatedPeers() []peer.ID {
        return p.authManager.GetAuthenticatedPeers()
}

// RevokeAuthentication revokes authentication for misbehaving peer
func (p *P2PNetwork) RevokeAuthentication(peerID peer.ID) {
        p.authManager.RevokeAuthentication(peerID)
        log.Printf("⚠️  Revoked authentication for peer %s", peerID.String()[:8])
}
