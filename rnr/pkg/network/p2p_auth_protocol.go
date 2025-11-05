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
)

const (
        AuthProtocol = "/rnr/auth/1.0.0"
)

// AuthMessage types for authentication protocol
type AuthMessageType int

const (
        AuthTypeChallenge AuthMessageType = iota
        AuthTypeResponse
        AuthTypeResult
)

// AuthMessage represents authentication protocol messages
type AuthMessage struct {
        Type      AuthMessageType
        PeerID    string
        Nonce     []byte
        Solution  []byte
        Success   bool
        Error     string
        Timestamp int64
}

// SetupAuthProtocol initializes authentication protocol handlers
func (p *P2PNetwork) SetupAuthProtocol() {
        p.Host.SetStreamHandler(protocol.ID(AuthProtocol), p.handleAuthStream)
        log.Printf("✅ P2P Authentication protocol handler registered")
}

// handleAuthStream processes incoming authentication requests
func (p *P2PNetwork) handleAuthStream(stream network.Stream) {
        defer stream.Close()

        remotePeer := stream.Conn().RemotePeer()
        
        // CRITICAL FIX: Use decoder to read without waiting for stream close
        decoder := json.NewDecoder(stream)
        
        var msg AuthMessage
        if err := decoder.Decode(&msg); err != nil {
                if err != io.EOF {
                        log.Printf("⚠️  Failed to decode auth message from %s: %v", remotePeer.String()[:8], err)
                }
                return
        }

        switch msg.Type {
        case AuthTypeChallenge:
                // Peer sent us a challenge - solve and respond
                p.handleAuthChallenge(stream, remotePeer, &msg)
                
        case AuthTypeResponse:
                // Peer sent challenge response - verify it
                p.handleAuthResponse(stream, remotePeer, &msg)
        }
}

// handleAuthChallenge handles incoming authentication challenge
func (p *P2PNetwork) handleAuthChallenge(stream network.Stream, remotePeer peer.ID, msg *AuthMessage) {
        // Solve the challenge
        solution := p.authManager.SolveChallenge(remotePeer, msg.Nonce)
        
        // Send response
        response := &AuthMessage{
                Type:      AuthTypeResponse,
                PeerID:    p.Host.ID().String(),
                Nonce:     msg.Nonce,
                Solution:  solution,
                Timestamp: time.Now().Unix(),
        }
        
        // CRITICAL FIX: Use encoder to write message properly
        encoder := json.NewEncoder(stream)
        if err := encoder.Encode(response); err != nil {
                log.Printf("⚠️  Failed to encode auth response: %v", err)
                return
        }
        
        log.Printf("📤 Sent authentication response to %s", remotePeer.String()[:8])
}

// handleAuthResponse handles incoming authentication response
func (p *P2PNetwork) handleAuthResponse(stream network.Stream, remotePeer peer.ID, msg *AuthMessage) {
        // Verify the response
        response := &ChallengeResponse{
                PeerID:    remotePeer,
                Nonce:     msg.Nonce,
                Solution:  msg.Solution,
                Timestamp: time.Unix(msg.Timestamp, 0),
        }
        
        err := p.authManager.VerifyResponse(response)
        
        // Send result back
        result := &AuthMessage{
                Type:      AuthTypeResult,
                Success:   err == nil,
                Error:     "",
                Timestamp: time.Now().Unix(),
        }
        
        if err != nil {
                result.Error = err.Error()
                log.Printf("❌ Authentication failed for %s: %v", remotePeer.String()[:8], err)
        } else {
                log.Printf("✅ Peer %s authenticated successfully via stream handler", remotePeer.String()[:8])
        }
        
        // CRITICAL FIX: Use encoder to write result properly
        encoder := json.NewEncoder(stream)
        encoder.Encode(result)
}

// AuthenticatePeerViaProtocol performs real authentication challenge-response
func (p *P2PNetwork) AuthenticatePeerViaProtocol(peerID peer.ID) error {
        // Check if already authenticated
        if p.authManager.IsAuthenticated(peerID) {
                return nil
        }

        // Generate challenge
        challenge, err := p.authManager.GenerateChallenge(peerID)
        if err != nil {
                return fmt.Errorf("failed to generate challenge: %w", err)
        }

        // Open stream to peer
        stream, err := p.Host.NewStream(p.ctx, peerID, protocol.ID(AuthProtocol))
        if err != nil {
                return fmt.Errorf("failed to open auth stream: %w", err)
        }
        defer stream.Close()

        // Set deadlines for the entire handshake
        stream.SetDeadline(time.Now().Add(10 * time.Second))

        // Send challenge using encoder
        challengeMsg := &AuthMessage{
                Type:      AuthTypeChallenge,
                PeerID:    p.Host.ID().String(),
                Nonce:     challenge.Nonce,
                Timestamp: challenge.Timestamp.Unix(),
        }

        encoder := json.NewEncoder(stream)
        if err := encoder.Encode(challengeMsg); err != nil {
                return fmt.Errorf("failed to encode challenge: %w", err)
        }

        // CRITICAL FIX: Use decoder to read response without waiting for stream close
        decoder := json.NewDecoder(stream)
        
        var response AuthMessage
        if err := decoder.Decode(&response); err != nil {
                return fmt.Errorf("failed to decode response: %w", err)
        }

        if response.Type != AuthTypeResponse {
                return fmt.Errorf("unexpected response type: %d", response.Type)
        }

        // Verify response
        challengeResponse := &ChallengeResponse{
                PeerID:    peerID,
                Nonce:     response.Nonce,
                Solution:  response.Solution,
                Timestamp: time.Unix(response.Timestamp, 0),
        }

        if err := p.authManager.VerifyResponse(challengeResponse); err != nil {
                return fmt.Errorf("authentication verification failed: %w", err)
        }

        log.Printf("✅ Successfully authenticated peer %s via protocol", peerID.String()[:8])
        return nil
}
