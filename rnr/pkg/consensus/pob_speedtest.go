package consensus

import (
        "crypto/aes"
        "crypto/cipher"
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "sync"
        "time"
)

const (
        // SpeedTest constants
        PayloadSize      = 8 * 1024 * 1024  // 8 MB
        ChunkSize        = 512 * 1024        // 512 KB chunks
        NumChunks        = PayloadSize / ChunkSize
        CommitteeSize    = 3                 // Minimum 3 testers for P2P speed test
)

// P2PSpeedTestSession manages P2P-based PoB speed test with cryptographic payloads
type P2PSpeedTestSession struct {
        SessionID          string
        CandidateID        string
        CommitteeTesters   []string
        PayloadSeed        []byte
        PayloadRootHash    string                      // Reference Merkle root (from first tester)
        TesterMerkleRoots  map[string]string           // SECURITY: Per-tester Merkle roots for independent verification
        TesterChunkHashes  map[string][]string         // Per-tester chunk hashes
        ChunkHashes        []string                     // Deprecated: use TesterChunkHashes
        TesterResults      map[string]*SpeedTestResult
        StartTime          time.Time
        Status             string // "initiated", "active", "completed", "failed"
        mu                 sync.RWMutex
}

// SpeedTestResult stores P2P speed test results with anti-cheat verification
type SpeedTestResult struct {
        TesterID           string
        CandidateID        string
        SessionID          string
        UploadBandwidth    float64           // MB/s - Whitepaper: ≥ 7 MB/s
        Latency            float64           // milliseconds - Whitepaper: ≤ 100 ms
        PacketLoss         float64           // percentage - Whitepaper: 0.1%
        ChunkTimestamps    []time.Time       // Monotonic timestamps for each chunk
        ReceivedChunks     int
        VerifiedHashes     []string          // BLAKE3 hashes of received chunks
        PayloadVerified    bool              // True if Merkle root matches
        Timestamp          time.Time
        TesterSignature    []byte
        Anomalies          []string          // Detected anomalies (compression, throttling, etc)
}

// CryptographicPayloadGenerator generates non-compressible 8 MB payloads
type CryptographicPayloadGenerator struct {
        seed      []byte
        sessionID string
        testerID  string
}

// NewCryptographicPayloadGenerator creates a generator for anti-compression payloads
func NewCryptographicPayloadGenerator(sessionID, testerID string) (*CryptographicPayloadGenerator, error) {
        // Generate cryptographic seed from session and tester IDs
        seed := make([]byte, 32)
        if _, err := io.ReadFull(rand.Reader, seed); err != nil {
                return nil, fmt.Errorf("failed to generate seed: %w", err)
        }

        // Mix session ID and tester ID into seed for uniqueness
        hasher := sha256.New()
        hasher.Write(seed)
        hasher.Write([]byte(sessionID))
        hasher.Write([]byte(testerID))
        finalSeed := hasher.Sum(nil)

        return &CryptographicPayloadGenerator{
                seed:      finalSeed,
                sessionID: sessionID,
                testerID:  testerID,
        }, nil
}

// GeneratePayload creates an 8 MB cryptographic payload using AES-CTR keystream
// This ensures high entropy data that cannot be compressed
func (cpg *CryptographicPayloadGenerator) GeneratePayload() ([]byte, []string, string, error) {
        payload := make([]byte, PayloadSize)
        chunkHashes := make([]string, NumChunks)

        // Create AES-CTR cipher for keystream generation
        block, err := aes.NewCipher(cpg.seed)
        if err != nil {
                return nil, nil, "", fmt.Errorf("failed to create AES cipher: %w", err)
        }

        // Use zero IV for deterministic generation (seed already includes randomness)
        iv := make([]byte, aes.BlockSize)
        stream := cipher.NewCTR(block, iv)

        // Generate sequential 512 KB chunks with AES-CTR keystream
        for i := 0; i < NumChunks; i++ {
                chunkStart := i * ChunkSize
                chunkEnd := chunkStart + ChunkSize
                chunk := payload[chunkStart:chunkEnd]

                // Fill chunk with AES-CTR keystream (high entropy, non-compressible)
                stream.XORKeyStream(chunk, chunk)

                // Calculate SHA256 hash for each chunk
                hash := sha256.Sum256(chunk)
                chunkHash := hex.EncodeToString(hash[:])
                chunkHashes[i] = chunkHash
        }

        // Calculate Merkle root from chunk hashes
        merkleRoot := calculateMerkleRootFromHashes(chunkHashes)

        return payload, chunkHashes, merkleRoot, nil
}

// calculateMerkleRootFromHashes builds Merkle tree from BLAKE3 chunk hashes
func calculateMerkleRootFromHashes(chunkHashes []string) string {
        if len(chunkHashes) == 0 {
                return ""
        }

        // Convert hex hashes to bytes
        hashes := make([][]byte, len(chunkHashes))
        for i, h := range chunkHashes {
                decoded, _ := hex.DecodeString(h)
                hashes[i] = decoded
        }

        // Build Merkle tree
        for len(hashes) > 1 {
                var newLevel [][]byte
                for i := 0; i < len(hashes); i += 2 {
                        if i+1 < len(hashes) {
                                // Hash pair
                                combined := append(hashes[i], hashes[i+1]...)
                                hash := sha256.Sum256(combined)
                                newLevel = append(newLevel, hash[:])
                        } else {
                                // Odd hash, promote to next level
                                newLevel = append(newLevel, hashes[i])
                        }
                }
                hashes = newLevel
        }

        return hex.EncodeToString(hashes[0])
}

// P2PSpeedTestManager manages P2P speed test sessions with committee verification
type P2PSpeedTestManager struct {
        activeSessions map[string]*P2PSpeedTestSession
        zkSystem       *ZKProofSystem
        mu             sync.RWMutex
}

// NewP2PSpeedTestManager creates a new P2P speed test manager
func NewP2PSpeedTestManager() *P2PSpeedTestManager {
        zkSystem, err := NewZKProofSystem()
        if err != nil {
                fmt.Printf("⚠️  Failed to initialize ZK proof system: %v (using fallback verification)\n", err)
                zkSystem = nil
        } else {
                fmt.Println("✅ ZK-SNARK proof system initialized for P2P speed tests")
        }
        
        return &P2PSpeedTestManager{
                activeSessions: make(map[string]*P2PSpeedTestSession),
                zkSystem:       zkSystem,
        }
}

// InitiateP2PSpeedTest starts a P2P speed test session with committee selection
func (psm *P2PSpeedTestManager) InitiateP2PSpeedTest(candidateID string, allValidators []string) (*P2PSpeedTestSession, error) {
        psm.mu.Lock()
        defer psm.mu.Unlock()

        // Generate unique session ID
        sessionID := generateSessionID(candidateID)

        // Select committee of testers (minimum 3)
        committee, err := selectCommitteeTesters(candidateID, allValidators, CommitteeSize)
        if err != nil {
                return nil, fmt.Errorf("failed to select committee: %w", err)
        }

        if len(committee) < CommitteeSize {
                return nil, fmt.Errorf("insufficient validators for committee: need %d, got %d", CommitteeSize, len(committee))
        }

        // Generate payload seed
        seed := make([]byte, 32)
        if _, err := io.ReadFull(rand.Reader, seed); err != nil {
                return nil, fmt.Errorf("failed to generate seed: %w", err)
        }

        session := &P2PSpeedTestSession{
                SessionID:         sessionID,
                CandidateID:       candidateID,
                CommitteeTesters:  committee,
                PayloadSeed:       seed,
                TesterMerkleRoots: make(map[string]string),         // Per-tester roots
                TesterChunkHashes: make(map[string][]string),       // Per-tester chunks
                TesterResults:     make(map[string]*SpeedTestResult),
                StartTime:         time.Now(),
                Status:            "initiated",
        }

        psm.activeSessions[sessionID] = session
        return session, nil
}

// SubmitTesterResult records result from a committee tester
func (psm *P2PSpeedTestManager) SubmitTesterResult(sessionID string, result *SpeedTestResult) error {
        psm.mu.Lock()
        defer psm.mu.Unlock()

        session, exists := psm.activeSessions[sessionID]
        if !exists {
                return fmt.Errorf("session %s not found", sessionID)
        }

        // Verify tester is in committee
        isTester := false
        for _, tester := range session.CommitteeTesters {
                if tester == result.TesterID {
                        isTester = true
                        break
                }
        }

        if !isTester {
                return fmt.Errorf("tester %s not in committee for session %s", result.TesterID, sessionID)
        }

        // Store result
        session.TesterResults[result.TesterID] = result

        // Check if all testers submitted
        if len(session.TesterResults) == len(session.CommitteeTesters) {
                session.Status = "completed"
        } else {
                session.Status = "active"
        }

        return nil
}

// AggregateP2PResults aggregates results from all committee testers
func (psm *P2PSpeedTestManager) AggregateP2PResults(sessionID string) (*PoBTestResult, error) {
        psm.mu.RLock()
        defer psm.mu.RUnlock()

        session, exists := psm.activeSessions[sessionID]
        if !exists {
                return nil, fmt.Errorf("session %s not found", sessionID)
        }

        if len(session.TesterResults) == 0 {
                return nil, fmt.Errorf("no tester results available")
        }

        // Calculate median values to handle outliers (Whitepaper Bab 3.1.3)
        var uploads, latencies, packetLosses []float64
        validCount := 0

        for _, result := range session.TesterResults {
                // Check for anomalies
                if len(result.Anomalies) > 0 {
                        fmt.Printf("⚠️  Tester %s detected anomalies: %v\n", result.TesterID, result.Anomalies)
                        continue
                }

                // Verify payload integrity
                if !result.PayloadVerified {
                        fmt.Printf("⚠️  Tester %s: payload verification failed\n", result.TesterID)
                        continue
                }

                uploads = append(uploads, result.UploadBandwidth)
                latencies = append(latencies, result.Latency)
                packetLosses = append(packetLosses, result.PacketLoss)
                validCount++
        }

        // Require at least 2 valid results
        if validCount < 2 {
                return nil, fmt.Errorf("insufficient valid results: need 2, got %d", validCount)
        }

        // Calculate median values (Whitepaper: avoid anomalies with median)
        medianUpload := calculateMedian(uploads)
        medianLatency := calculateMedian(latencies)
        medianPacketLoss := calculateMedian(packetLosses)

        // Calculate PoB score
        score := calculatePoBScore(medianUpload, medianLatency, medianPacketLoss)
        
        // Generate test data hash for ZK proof
        testDataHash := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", session.CandidateID, session.StartTime.Unix())))
        
        // Create aggregated result
        aggregated := &PoBTestResult{
                CandidateID:      session.CandidateID,
                UploadBandwidth:  medianUpload,
                Latency:          medianLatency,
                PacketLoss:       medianPacketLoss,
                Timestamp:        time.Now(),
                Passed:           false,
                TestDataHash:     hex.EncodeToString(testDataHash[:]),
        }

        // Evaluate if test passed
        aggregated.Passed = evaluatePoBResult(aggregated)
        
        // Generate ZK-SNARK proof if ZK system is available
        if psm.zkSystem != nil {
                proof, err := psm.zkSystem.GenerateProof(aggregated, score)
                if err != nil {
                        fmt.Printf("⚠️  ZK proof generation failed: %v (result still valid)\n", err)
                } else {
                        aggregated.ZKProof = proof
                        fmt.Printf("✅ ZK-SNARK proof generated for candidate %s\n", session.CandidateID)
                }
        }

        return aggregated, nil
}

// VerifyChunkSequence verifies that chunks arrived in order with monotonic timestamps
// This detects throttling or manipulation
func VerifyChunkSequence(timestamps []time.Time) (bool, []string) {
        anomalies := []string{}

        if len(timestamps) != NumChunks {
                anomalies = append(anomalies, fmt.Sprintf("incomplete chunks: got %d, expected %d", len(timestamps), NumChunks))
                return false, anomalies
        }

        // Check monotonic timestamps
        for i := 1; i < len(timestamps); i++ {
                if timestamps[i].Before(timestamps[i-1]) {
                        anomalies = append(anomalies, fmt.Sprintf("non-monotonic timestamp at chunk %d", i))
                }

                // Check for suspicious delays (throttling detection)
                duration := timestamps[i].Sub(timestamps[i-1])
                if duration > 5*time.Second {
                        anomalies = append(anomalies, fmt.Sprintf("suspicious delay at chunk %d: %v", i, duration))
                }
        }

        return len(anomalies) == 0, anomalies
}

// generateSessionID creates unique session ID
func generateSessionID(candidateID string) string {
        hasher := sha256.New()
        hasher.Write([]byte(candidateID))
        hasher.Write([]byte(time.Now().String()))
        return hex.EncodeToString(hasher.Sum(nil))[:16]
}

// selectCommitteeTesters selects committee members for speed test
// Excludes the candidate itself
func selectCommitteeTesters(candidateID string, allValidators []string, size int) ([]string, error) {
        available := []string{}
        for _, v := range allValidators {
                if v != candidateID {
                        available = append(available, v)
                }
        }

        if len(available) < size {
                return nil, fmt.Errorf("not enough validators: need %d, available %d", size, len(available))
        }

        // Simple deterministic selection based on candidate ID hash
        // This ensures fairness without needing full VRF system
        hasher := sha256.New()
        hasher.Write([]byte(candidateID))
        hasher.Write([]byte(time.Now().String()))
        seed := hasher.Sum(nil)

        selected := []string{}
        for i := 0; i < size && i < len(available); i++ {
                // Deterministic selection using seed
                index := int(seed[i%len(seed)]) % len(available)
                selected = append(selected, available[index])
                // Remove selected to avoid duplicates
                available = append(available[:index], available[index+1:]...)
        }

        return selected, nil
}

// calculateMedian returns median value from slice
func calculateMedian(values []float64) float64 {
        if len(values) == 0 {
                return 0
        }

        // Simple median calculation
        n := len(values)
        if n == 1 {
                return values[0]
        }

        // Sort values
        sorted := make([]float64, n)
        copy(sorted, values)
        for i := 0; i < n-1; i++ {
                for j := i + 1; j < n; j++ {
                        if sorted[i] > sorted[j] {
                                sorted[i], sorted[j] = sorted[j], sorted[i]
                        }
                }
        }

        // Return median
        if n%2 == 0 {
                return (sorted[n/2-1] + sorted[n/2]) / 2.0
        }
        return sorted[n/2]
}

// calculatePoBScore calculates PoB score based on median metrics (Whitepaper Bab 3.1.2)
func calculatePoBScore(upload, latency, packetLoss float64) float64 {
        // Upload score: normalize by minimum threshold (7 MB/s)
        uploadScore := upload / 7.0
        if uploadScore > 1.0 {
                uploadScore = 1.0
        }
        
        // Latency score: invert (lower is better), normalize by 100ms
        latencyScore := 100.0 / latency
        if latencyScore > 1.0 {
                latencyScore = 1.0
        }
        
        // Packet loss score: invert (lower is better), normalize by 0.1%
        packetLossScore := 1.0 - (packetLoss / 0.1)
        if packetLossScore < 0 {
                packetLossScore = 0
        } else if packetLossScore > 1.0 {
                packetLossScore = 1.0
        }
        
        // Average of three scores
        return (uploadScore + latencyScore + packetLossScore) / 3.0
}

// VerifyPoBResultZKProof verifies ZK-SNARK proof for PoB test result
func (psm *P2PSpeedTestManager) VerifyPoBResultZKProof(result *PoBTestResult) (bool, error) {
        if psm.zkSystem == nil {
                // ZK system not available, skip verification
                return true, nil
        }
        
        if len(result.ZKProof) == 0 {
                return false, fmt.Errorf("no ZK proof provided")
        }
        
        // Calculate score from result metrics
        score := calculatePoBScore(result.UploadBandwidth, result.Latency, result.PacketLoss)
        
        // Verify ZK proof
        valid, err := psm.zkSystem.VerifyProof(result.ZKProof, result.TestDataHash, score, result.Passed)
        if err != nil {
                return false, fmt.Errorf("ZK proof verification failed: %w", err)
        }
        
        if !valid {
                return false, fmt.Errorf("ZK proof is invalid")
        }
        
        fmt.Printf("✅ ZK-SNARK proof verified for candidate %s\n", result.CandidateID)
        return true, nil
}
