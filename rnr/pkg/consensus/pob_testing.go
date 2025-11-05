package consensus

import (
        "crypto/rand"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "io"
        "net"
        "time"

        "rnr-blockchain/pkg/core"
)

type PoBTestResult struct {
        CandidateID      string
        TesterID         string
        UploadBandwidth  float64 // MB/s - Whitepaper: ≥ 7 MB/s
        Latency          float64 // ms - Whitepaper: ≤ 100 ms
        PacketLoss       float64 // % - Whitepaper: 0.1%
        TestDataHash     string
        MerkleRoot       string
        TransmissionTime uint64
        Timestamp        time.Time
        Passed           bool
        ZKProof          []byte
}

type PoBTestManager struct {
        activeTests  map[string]*PoBTestSession
        zkSystem     *ZKProofSystem
        antiDRDoS    *AntiDRDoSManager
}

type PoBTestSession struct {
        CandidateID    string
        Testers        []string
        TestData       []byte
        CommitmentHash string
        Results        map[string]*PoBTestResult
        StartTime      time.Time
}

func NewPoBTestManager() *PoBTestManager {
        zkSystem, err := NewZKProofSystem()
        if err != nil {
                fmt.Printf("⚠️  Failed to initialize ZK proof system: %v (using fallback verification)\n", err)
                zkSystem = nil
        } else {
                fmt.Println("✅ ZK-SNARK proof system initialized")
        }
        
        antiDRDoS := NewAntiDRDoSManager()
        fmt.Println("✅ Anti-DRDoS protection enabled")
        
        return &PoBTestManager{
                activeTests:  make(map[string]*PoBTestSession),
                zkSystem:     zkSystem,
                antiDRDoS:    antiDRDoS,
        }
}

func (ptm *PoBTestManager) InitiateTest(candidateID string, testers []string) (*PoBTestSession, error) {
        for _, testerID := range testers {
                remaining := ptm.antiDRDoS.GetRemainingTests(testerID)
                if remaining == 0 {
                        return nil, fmt.Errorf("tester %s exceeded rate limit", testerID)
                }
        }
        
        testData := make([]byte, core.TestDataSize)
        if _, err := rand.Read(testData); err != nil {
                return nil, fmt.Errorf("failed to generate cryptographic test data: %w", err)
        }

        hash := sha256.Sum256(testData)
        commitmentHash := hex.EncodeToString(hash[:])

        session := &PoBTestSession{
                CandidateID:    candidateID,
                Testers:        testers,
                TestData:       testData,
                CommitmentHash: commitmentHash,
                Results:        make(map[string]*PoBTestResult),
                StartTime:      time.Now(),
        }

        ptm.activeTests[candidateID] = session
        return session, nil
}

func (ptm *PoBTestManager) RequestBandwidthTest(candidateID, testerID string, validatorReputation float64) (*Challenge, error) {
        if !ptm.antiDRDoS.CheckReputationThreshold(validatorReputation) {
                return nil, fmt.Errorf("tester reputation %.2f below minimum threshold", validatorReputation)
        }
        
        challenge, err := ptm.antiDRDoS.GenerateChallenge(candidateID, testerID)
        if err != nil {
                return nil, fmt.Errorf("failed to generate challenge: %w", err)
        }
        
        return challenge, nil
}

func (ptm *PoBTestManager) RespondToChallenge(challenge *Challenge) (string, error) {
        responseHash := ptm.antiDRDoS.ComputeChallengeResponse(
                challenge.CandidateID,
                challenge.TesterID,
                challenge.Nonce,
        )
        return responseHash, nil
}

func (ptm *PoBTestManager) VerifyAndStartTest(challengeID, responseHash string) (bool, error) {
        verified, err := ptm.antiDRDoS.VerifyChallenge(challengeID, responseHash)
        if err != nil {
                return false, fmt.Errorf("challenge verification failed: %w", err)
        }
        
        if !verified {
                return false, fmt.Errorf("invalid challenge response")
        }
        
        return true, nil
}

func ConductBandwidthTest(candidateAddr string, testData []byte, timeout time.Duration) (*PoBTestResult, error) {
        result := &PoBTestResult{
                CandidateID: candidateAddr,
                Timestamp:   time.Now(),
        }

        conn, err := net.DialTimeout("tcp", candidateAddr, 5*time.Second)
        if err != nil {
                return nil, fmt.Errorf("failed to connect to candidate: %w", err)
        }
        defer conn.Close()

        // Measure Latency and Packet Loss (Whitepaper Bab 3.1.2)
        numPings := 100
        latencyMeasurements := make([]float64, 0, numPings)
        packetsLost := 0

        for i := 0; i < numPings; i++ {
                latencyStart := time.Now()
                pingData := []byte("PING")
                _, err = conn.Write(pingData)
                if err != nil {
                        packetsLost++
                        continue
                }

                response := make([]byte, 4)
                conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
                _, err = conn.Read(response)
                if err != nil {
                        packetsLost++ // Packet lost (timeout or error)
                        continue
                }
                latency := time.Since(latencyStart).Milliseconds()
                latencyMeasurements = append(latencyMeasurements, float64(latency))
        }

        // Calculate average latency from successful pings
        avgLatency := 0.0
        if len(latencyMeasurements) > 0 {
                for _, lat := range latencyMeasurements {
                        avgLatency += lat
                }
                avgLatency /= float64(len(latencyMeasurements))
        }
        result.Latency = avgLatency

        // Calculate packet loss percentage (Whitepaper: Target 0.1%)
        result.PacketLoss = (float64(packetsLost) / float64(numPings)) * 100.0

        uploadStart := time.Now()
        totalSent := 0

        chunkSize := 64 * 1024
        for i := 0; i < len(testData); i += chunkSize {
                end := i + chunkSize
                if end > len(testData) {
                        end = len(testData)
                }

                chunk := testData[i:end]
                n, err := conn.Write(chunk)
                if err != nil {
                        return nil, fmt.Errorf("upload error: %w", err)
                }
                totalSent += n

                ack := make([]byte, 3)
                conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
                _, err = conn.Read(ack)
                if err != nil {
                        return nil, fmt.Errorf("ack timeout: %w", err)
                }
        }

        uploadTime := time.Since(uploadStart).Milliseconds()
        result.TransmissionTime = uint64(uploadTime)

        uploadMBps := float64(totalSent) / (1024 * 1024) / (float64(uploadTime) / 1000.0)
        result.UploadBandwidth = uploadMBps

        hash := sha256.Sum256(testData)
        result.TestDataHash = hex.EncodeToString(hash[:])
        result.MerkleRoot = calculateMerkleRoot(testData)

        result.Passed = evaluatePoBResult(result)

        return result, nil
}

func (ptm *PoBTestManager) GenerateZKProof(result *PoBTestResult) error {
        if ptm.zkSystem == nil {
                return fmt.Errorf("ZK proof system not initialized")
        }
        
        score := CalculatePoBScore([]*PoBTestResult{result})
        proof, err := ptm.zkSystem.GenerateProof(result, score)
        if err != nil {
                return fmt.Errorf("failed to generate ZK proof: %w", err)
        }
        
        result.ZKProof = proof
        return nil
}

func (ptm *PoBTestManager) VerifyZKProof(result *PoBTestResult) (bool, error) {
        if ptm.zkSystem == nil {
                return false, fmt.Errorf("ZK proof system not initialized")
        }
        
        if len(result.ZKProof) == 0 {
                return false, fmt.Errorf("no ZK proof provided")
        }
        
        score := CalculatePoBScore([]*PoBTestResult{result})
        return ptm.zkSystem.VerifyProof(result.ZKProof, result.TestDataHash, score, result.Passed)
}

func evaluatePoBResult(result *PoBTestResult) bool {
        // Whitepaper Bab 3.1.2 requirements
        if result.UploadBandwidth < core.MinUploadBandwidth { // ≥ 7 MB/s
                return false
        }
        if result.Latency > core.TargetLatency { // ≤ 100 ms
                return false
        }
        if result.PacketLoss > core.TargetPacketLoss { // 0.1%
                return false
        }
        return true
}

func CalculatePoBScore(results []*PoBTestResult) float64 {
        if len(results) == 0 {
                return 0.0
        }

        var avgUpload, avgLatency, avgPacketLoss float64
        validResults := 0

        for _, result := range results {
                if result.Passed {
                        avgUpload += result.UploadBandwidth
                        avgLatency += result.Latency
                        avgPacketLoss += result.PacketLoss
                        validResults++
                }
        }

        if validResults == 0 {
                return 0.0
        }

        avgUpload /= float64(validResults)
        avgLatency /= float64(validResults)
        avgPacketLoss /= float64(validResults)

        // Whitepaper Bab 3.1.2 scoring
        uploadScore := avgUpload / core.MinUploadBandwidth // ≥ 7 MB/s
        if uploadScore > 1.0 {
                uploadScore = 1.0
        }

        latencyScore := core.TargetLatency / avgLatency // ≤ 100 ms
        if latencyScore > 1.0 {
                latencyScore = 1.0
        }

        // Packet Loss: lower is better (Target: 0.1%)
        packetLossScore := 1.0 - (avgPacketLoss / core.TargetPacketLoss)
        if packetLossScore < 0 {
                packetLossScore = 0
        }
        if packetLossScore > 1.0 {
                packetLossScore = 1.0
        }

        // Average of three metrics (Upload, Latency, Packet Loss)
        score := (uploadScore + latencyScore + packetLossScore) / 3.0
        return score
}

func calculateMerkleRoot(data []byte) string {
        chunkSize := 1024
        hashes := make([][]byte, 0)

        for i := 0; i < len(data); i += chunkSize {
                end := i + chunkSize
                if end > len(data) {
                        end = len(data)
                }
                chunk := data[i:end]
                hash := sha256.Sum256(chunk)
                hashes = append(hashes, hash[:])
        }

        for len(hashes) > 1 {
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
                hashes = newHashes
        }

        if len(hashes) > 0 {
                return hex.EncodeToString(hashes[0])
        }
        return ""
}

func HandlePoBTestRequest(conn net.Conn) error {
        defer conn.Close()

        // Phase 1: Respond to PING requests for latency measurement
        // ConductBandwidthTest sends ~100 PINGs sequentially
        pingBuf := make([]byte, 4)
        for {
                n, err := conn.Read(pingBuf)
                if err != nil {
                        if err == io.EOF {
                                return nil
                        }
                        return err
                }

                if n == 4 && string(pingBuf) == "PING" {
                        // Respond with PONG for latency measurement
                        _, err = conn.Write([]byte("PONG"))
                        if err != nil {
                                return err
                        }
                } else {
                        // Not a PING - must be start of payload chunks
                        // Unread this data and move to chunk phase
                        break
                }
        }

        // Phase 2: Receive payload chunks and send ACKs
        for {
                chunk := make([]byte, 64*1024)
                n, err := conn.Read(chunk)
                if err != nil {
                        if err == io.EOF {
                                break
                        }
                        return err
                }

                _, err = conn.Write([]byte("ACK"))
                if err != nil {
                        return err
                }

                if n < len(chunk) {
                        break
                }
        }

        return nil
}

func (ptm *PoBTestManager) AggregateResults(candidateID string) (float64, error) {
        session, exists := ptm.activeTests[candidateID]
        if !exists {
                return 0, fmt.Errorf("no test session found for candidate %s", candidateID)
        }

        results := make([]*PoBTestResult, 0, len(session.Results))
        for _, result := range session.Results {
                // Generate ZK proof for each result if ZK system available
                if ptm.zkSystem != nil && len(result.ZKProof) == 0 {
                        resultScore := CalculatePoBScore([]*PoBTestResult{result})
                        proof, err := ptm.zkSystem.GenerateProof(result, resultScore)
                        if err != nil {
                                fmt.Printf("⚠️  ZK proof generation failed: %v\n", err)
                        } else {
                                result.ZKProof = proof
                                fmt.Printf("✅ ZK-SNARK proof generated for PoB result\n")
                        }
                }
                
                results = append(results, result)
        }

        score := CalculatePoBScore(results)
        return score, nil
}
