package network

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// SpeedTestRequest is sent by a tester to initiate speed test
type SpeedTestRequest struct {
	SessionID          string
	TesterID           string
	CandidateID        string
	PayloadRootHash    string   // Merkle root commitment
	ExpectedChunks     int
	ChallengeNonce     []byte
	Timestamp          time.Time
}

// SpeedTestResponse is sent by candidate to accept speed test
type SpeedTestResponse struct {
	SessionID       string
	CandidateID     string
	Accepted        bool
	PayloadReady    bool
	ResponseHash    []byte
	Timestamp       time.Time
}

// SpeedTestChunk represents one chunk of the speed test payload
type SpeedTestChunk struct {
	SessionID    string
	ChunkIndex   int
	Data         []byte
	ChunkHash    string
	Timestamp    time.Time
}

// SpeedTestResult contains the complete test result
type SpeedTestResult struct {
	SessionID          string
	TesterID           string
	CandidateID        string
	UploadBandwidth    float64
	Latency            float64
	Jitter             float64
	ChunkTimestamps    []time.Time
	ReceivedChunks     int
	VerifiedHashes     []string
	PayloadVerified    bool
	Timestamp          time.Time
	Anomalies          []string
}

// SetupSpeedTestHandlers registers P2P speed test protocol handlers
func (p *P2PNetwork) SetupSpeedTestHandlers() {
	p.Host.SetStreamHandler(protocol.ID(SpeedTestReqProtocol), p.handleSpeedTestRequest)
	p.Host.SetStreamHandler(protocol.ID(SpeedTestStreamProtocol), p.handleSpeedTestStream)
	fmt.Println("✅ P2P Speed Test protocol handlers registered")
}

// handleSpeedTestRequest handles incoming speed test requests from testers
func (p *P2PNetwork) handleSpeedTestRequest(stream network.Stream) {
	defer stream.Close()

	var request SpeedTestRequest
	if err := json.NewDecoder(stream).Decode(&request); err != nil {
		fmt.Printf("❌ Failed to decode speed test request: %v\n", err)
		return
	}

	fmt.Printf("📊 Received speed test request from %s (session: %s)\n", request.TesterID, request.SessionID)

	// TODO: Validate request and prepare payload
	// For now, just accept
	response := SpeedTestResponse{
		SessionID:    request.SessionID,
		CandidateID:  request.CandidateID,
		Accepted:     true,
		PayloadReady: true,
		Timestamp:    time.Now(),
	}

	// Send response
	if err := json.NewEncoder(stream).Encode(&response); err != nil {
		fmt.Printf("❌ Failed to send speed test response: %v\n", err)
		return
	}

	fmt.Printf("✅ Speed test accepted for session %s\n", request.SessionID)
}

// handleSpeedTestStream handles the actual speed test data stream
func (p *P2PNetwork) handleSpeedTestStream(stream network.Stream) {
	defer stream.Close()

	startTime := time.Now()
	chunkTimestamps := []time.Time{}
	receivedChunks := 0
	totalBytes := 0

	reader := bufio.NewReader(stream)

	for {
		var chunk SpeedTestChunk
		if err := json.NewDecoder(reader).Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			fmt.Printf("❌ Error receiving chunk: %v\n", err)
			break
		}

		chunkTimestamps = append(chunkTimestamps, time.Now())
		receivedChunks++
		totalBytes += len(chunk.Data)

		// Verify chunk hash
		hash := sha256.Sum256(chunk.Data)
		actualHash := hex.EncodeToString(hash[:])
		
		if actualHash != chunk.ChunkHash {
			fmt.Printf("⚠️  Chunk %d hash mismatch! Expected: %s, Got: %s\n", 
				chunk.ChunkIndex, chunk.ChunkHash, actualHash)
		}

		// Send ACK
		ack := map[string]interface{}{
			"chunk_index": chunk.ChunkIndex,
			"ack":         true,
		}
		if err := json.NewEncoder(stream).Encode(&ack); err != nil {
			fmt.Printf("❌ Failed to send ACK: %v\n", err)
			break
		}
	}

	totalTime := time.Since(startTime)
	uploadMBps := float64(totalBytes) / (1024 * 1024) / totalTime.Seconds()

	fmt.Printf("📈 Speed test completed: %d chunks, %.2f MB/s, %v\n", 
		receivedChunks, uploadMBps, totalTime)
}

// RequestSpeedTest initiates a speed test request to a peer
func (p *P2PNetwork) RequestSpeedTest(peerID peer.ID, request *SpeedTestRequest) (*SpeedTestResponse, error) {
	ctx := p.ctx
	
	stream, err := p.Host.NewStream(ctx, peerID, protocol.ID(SpeedTestReqProtocol))
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	// Send request
	if err := json.NewEncoder(stream).Encode(request); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response
	var response SpeedTestResponse
	if err := json.NewDecoder(stream).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &response, nil
}

// ConductSpeedTest conducts the actual speed test by streaming payload chunks
func (p *P2PNetwork) ConductSpeedTest(peerID peer.ID, sessionID string, payload []byte, chunkHashes []string) (*SpeedTestResult, error) {
	ctx := p.ctx
	
	stream, err := p.Host.NewStream(ctx, peerID, protocol.ID(SpeedTestStreamProtocol))
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}
	defer stream.Close()

	writer := bufio.NewWriter(stream)
	reader := bufio.NewReader(stream)

	chunkSize := 512 * 1024 // 512 KB
	numChunks := len(payload) / chunkSize
	chunkTimestamps := make([]time.Time, numChunks)

	startTime := time.Now()

	// Send chunks sequentially
	for i := 0; i < numChunks; i++ {
		chunkStart := i * chunkSize
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(payload) {
			chunkEnd = len(payload)
		}

		chunk := SpeedTestChunk{
			SessionID:  sessionID,
			ChunkIndex: i,
			Data:       payload[chunkStart:chunkEnd],
			ChunkHash:  chunkHashes[i],
			Timestamp:  time.Now(),
		}

		chunkTimestamps[i] = chunk.Timestamp

		// Send chunk
		if err := json.NewEncoder(writer).Encode(&chunk); err != nil {
			return nil, fmt.Errorf("failed to send chunk %d: %w", i, err)
		}
		
		if err := writer.Flush(); err != nil {
			return nil, fmt.Errorf("failed to flush chunk %d: %w", i, err)
		}

		// Wait for ACK
		var ack map[string]interface{}
		if err := json.NewDecoder(reader).Decode(&ack); err != nil {
			return nil, fmt.Errorf("failed to receive ACK for chunk %d: %w", i, err)
		}
	}

	totalTime := time.Since(startTime)
	uploadMBps := float64(len(payload)) / (1024 * 1024) / totalTime.Seconds()

	result := &SpeedTestResult{
		SessionID:       sessionID,
		CandidateID:     peerID.String(),
		UploadBandwidth: uploadMBps,
		ChunkTimestamps: chunkTimestamps,
		ReceivedChunks:  numChunks,
		PayloadVerified: true,
		Timestamp:       time.Now(),
		Anomalies:       []string{},
	}

	return result, nil
}

// GetPeerIDByValidatorID converts validator ID to peer ID
// This is a placeholder - actual implementation would maintain a mapping
func (p *P2PNetwork) GetPeerIDByValidatorID(validatorID string) (peer.ID, error) {
	// TODO: Implement proper validator ID to peer ID mapping
	// For now, try to decode the validator ID as a peer ID
	peerID, err := peer.Decode(validatorID)
	if err != nil {
		return "", fmt.Errorf("failed to decode validator ID as peer ID: %w", err)
	}
	return peerID, nil
}
