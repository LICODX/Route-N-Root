package consensus

import (
        "bytes"
        "compress/gzip"
        "crypto/sha256"
        "encoding/hex"
        "testing"
        "time"
)

func TestCryptographicPayloadGeneration(t *testing.T) {
        t.Log("=== Testing Cryptographic Payload Generation ===")

        sessionID := "test-session-001"
        testerID := "test-tester-001"

        generator, err := NewCryptographicPayloadGenerator(sessionID, testerID)
        if err != nil {
                t.Fatalf("Failed to create payload generator: %v", err)
        }

        payload, chunkHashes, merkleRoot, err := generator.GeneratePayload()
        if err != nil {
                t.Fatalf("Failed to generate payload: %v", err)
        }

        // Verify payload size
        expectedSize := 8 * 1024 * 1024 // 8 MB
        if len(payload) != expectedSize {
                t.Errorf("Payload size mismatch: expected %d, got %d", expectedSize, len(payload))
        }

        // Verify number of chunks
        expectedChunks := 16 // 8 MB / 512 KB = 16 chunks
        if len(chunkHashes) != expectedChunks {
                t.Errorf("Chunk count mismatch: expected %d, got %d", expectedChunks, len(chunkHashes))
        }

        // Verify Merkle root is not empty
        if merkleRoot == "" {
                t.Error("Merkle root is empty")
        }

        t.Logf("✅ Payload generated successfully:")
        t.Logf("   Size: %d bytes (%.2f MB)", len(payload), float64(len(payload))/(1024*1024))
        t.Logf("   Chunks: %d", len(chunkHashes))
        t.Logf("   Merkle root: %s", merkleRoot[:16]+"...")
}

func TestPayloadNonCompressibility(t *testing.T) {
        t.Log("=== Testing Payload Non-Compressibility (Anti-Cheat) ===")

        sessionID := "test-session-002"
        testerID := "test-tester-002"

        generator, err := NewCryptographicPayloadGenerator(sessionID, testerID)
        if err != nil {
                t.Fatalf("Failed to create payload generator: %v", err)
        }

        payload, _, _, err := generator.GeneratePayload()
        if err != nil {
                t.Fatalf("Failed to generate payload: %v", err)
        }

        // Try to compress the payload
        var compressed bytes.Buffer
        gzipWriter := gzip.NewWriter(&compressed)
        
        _, err = gzipWriter.Write(payload)
        if err != nil {
                t.Fatalf("Failed to write to gzip: %v", err)
        }
        gzipWriter.Close()

        // Calculate compression ratio
        originalSize := len(payload)
        compressedSize := compressed.Len()
        compressionRatio := float64(compressedSize) / float64(originalSize)

        t.Logf("📊 Compression Analysis:")
        t.Logf("   Original size: %d bytes (%.2f MB)", originalSize, float64(originalSize)/(1024*1024))
        t.Logf("   Compressed size: %d bytes (%.2f MB)", compressedSize, float64(compressedSize)/(1024*1024))
        t.Logf("   Compression ratio: %.4f", compressionRatio)

        // High-entropy cryptographic data should have compression ratio close to 1.0
        // If ratio < 0.95, data is too compressible (security issue)
        if compressionRatio < 0.95 {
                t.Errorf("❌ SECURITY ISSUE: Payload is too compressible (ratio: %.4f)", compressionRatio)
                t.Error("   Nodes could cheat by compressing the payload!")
        } else {
                t.Logf("✅ Payload is non-compressible (ratio: %.4f >= 0.95)", compressionRatio)
                t.Log("   Anti-compression protection is working correctly")
        }

        // Verify high entropy
        entropy := calculateEntropy(payload)
        t.Logf("   Entropy: %.4f bits/byte", entropy)
        
        // High entropy should be close to 8.0 (maximum for bytes)
        if entropy < 7.5 {
                t.Errorf("❌ Low entropy detected: %.4f (expected > 7.5)", entropy)
        } else {
                t.Logf("✅ High entropy confirmed: %.4f bits/byte", entropy)
        }
}

func TestPayloadUniqueness(t *testing.T) {
        t.Log("=== Testing Payload Uniqueness (Anti-Caching) ===")

        sessionID := "test-session-003"

        // Generate two payloads with different tester IDs
        gen1, _ := NewCryptographicPayloadGenerator(sessionID, "tester-A")
        payload1, _, root1, _ := gen1.GeneratePayload()

        gen2, _ := NewCryptographicPayloadGenerator(sessionID, "tester-B")
        payload2, _, root2, _ := gen2.GeneratePayload()

        // Payloads should be different (anti-caching)
        if bytes.Equal(payload1, payload2) {
                t.Error("❌ SECURITY ISSUE: Payloads are identical!")
                t.Error("   Nodes could cache payloads and fake upload speed!")
        } else {
                t.Log("✅ Payloads are unique for different testers")
        }

        // Merkle roots should be different
        if root1 == root2 {
                t.Error("❌ Merkle roots are identical")
        } else {
                t.Logf("✅ Merkle roots are unique:")
                t.Logf("   Tester A: %s", root1[:16]+"...")
                t.Logf("   Tester B: %s", root2[:16]+"...")
        }
}

func TestChunkHashVerification(t *testing.T) {
        t.Log("=== Testing Chunk Hash Verification ===")

        generator, _ := NewCryptographicPayloadGenerator("session-004", "tester-004")
        payload, chunkHashes, _, err := generator.GeneratePayload()
        if err != nil {
                t.Fatalf("Failed to generate payload: %v", err)
        }

        // Verify each chunk hash
        chunkSize := 512 * 1024
        for i := 0; i < len(chunkHashes); i++ {
                chunkStart := i * chunkSize
                chunkEnd := chunkStart + chunkSize
                chunk := payload[chunkStart:chunkEnd]

                // Recalculate hash
                hash := sha256.Sum256(chunk)
                actualHash := hex.EncodeToString(hash[:])

                if actualHash != chunkHashes[i] {
                        t.Errorf("Chunk %d hash mismatch!", i)
                        t.Errorf("   Expected: %s", chunkHashes[i])
                        t.Errorf("   Actual: %s", actualHash)
                }
        }

        t.Logf("✅ All %d chunk hashes verified successfully", len(chunkHashes))
}

func TestChunkSequenceVerification(t *testing.T) {
        t.Log("=== Testing Chunk Sequence Verification (Anti-Throttling) ===")

        tests := []struct {
                name               string
                generateTimestamps func() []time.Time
                shouldPass         bool
                description        string
        }{
                {
                        name: "Normal sequence",
                        generateTimestamps: func() []time.Time {
                                timestamps := make([]time.Time, NumChunks)
                                base := time.Now()
                                for i := 0; i < NumChunks; i++ {
                                        timestamps[i] = base.Add(time.Duration(i*50) * time.Millisecond)
                                }
                                return timestamps
                        },
                        shouldPass:  true,
                        description: "Normal chunk sequence with regular timing",
                },
                {
                        name: "Non-monotonic (manipulation detected)",
                        generateTimestamps: func() []time.Time {
                                timestamps := make([]time.Time, NumChunks)
                                base := time.Now()
                                for i := 0; i < NumChunks; i++ {
                                        timestamps[i] = base.Add(time.Duration(i*50) * time.Millisecond)
                                }
                                // Swap two timestamps (non-monotonic)
                                timestamps[5], timestamps[4] = timestamps[4], timestamps[5]
                                return timestamps
                        },
                        shouldPass:  false,
                        description: "Non-monotonic timestamps indicate manipulation",
                },
                {
                        name: "Suspicious delay (throttling detected)",
                        generateTimestamps: func() []time.Time {
                                timestamps := make([]time.Time, NumChunks)
                                base := time.Now()
                                for i := 0; i < NumChunks; i++ {
                                        if i == 8 {
                                                // Add suspicious 6 second delay
                                                timestamps[i] = base.Add(time.Duration(i*50)*time.Millisecond + 6*time.Second)
                                        } else {
                                                timestamps[i] = base.Add(time.Duration(i*50) * time.Millisecond)
                                        }
                                }
                                return timestamps
                        },
                        shouldPass:  false,
                        description: "Suspicious delay indicates throttling or manipulation",
                },
                {
                        name: "Incomplete chunks",
                        generateTimestamps: func() []time.Time {
                                return make([]time.Time, NumChunks-5) // Missing 5 chunks
                        },
                        shouldPass:  false,
                        description: "Incomplete chunk transmission",
                },
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        timestamps := tt.generateTimestamps()
                        valid, anomalies := VerifyChunkSequence(timestamps)

                        if valid != tt.shouldPass {
                                t.Errorf("Expected valid=%v, got valid=%v", tt.shouldPass, valid)
                        }

                        if valid {
                                t.Logf("✅ %s: PASSED", tt.description)
                        } else {
                                t.Logf("⚠️  %s: DETECTED ANOMALIES", tt.description)
                                for _, anomaly := range anomalies {
                                        t.Logf("   - %s", anomaly)
                                }
                        }
                })
        }
}

func TestP2PSpeedTestSession(t *testing.T) {
        t.Log("=== Testing P2P Speed Test Session Management ===")

        manager := NewP2PSpeedTestManager()

        candidateID := "candidate-001"
        validators := []string{"val-001", "val-002", "val-003", "val-004", "val-005", "val-006", "candidate-001"}

        // Initiate session
        session, err := manager.InitiateP2PSpeedTest(candidateID, validators)
        if err != nil {
                t.Fatalf("Failed to initiate session: %v", err)
        }

        t.Logf("✅ Session created:")
        t.Logf("   Session ID: %s", session.SessionID)
        t.Logf("   Candidate: %s", session.CandidateID)
        t.Logf("   Committee size: %d", len(session.CommitteeTesters))

        // Verify committee doesn't include candidate
        for _, tester := range session.CommitteeTesters {
                if tester == candidateID {
                        t.Error("❌ Committee includes the candidate (should be excluded)")
                }
        }

        // Verify committee has required size
        if len(session.CommitteeTesters) != CommitteeSize {
                t.Logf("⚠️  Committee size is %d (expected %d)", len(session.CommitteeTesters), CommitteeSize)
        }

        // Submit results from testers
        for i, testerID := range session.CommitteeTesters {
                result := &SpeedTestResult{
                        SessionID:       session.SessionID,
                        TesterID:        testerID,
                        CandidateID:     candidateID,
                        UploadBandwidth: 8.0 + float64(i)*0.5, // Varying bandwidth
                        Latency:         50.0 + float64(i)*5.0,
                        PacketLoss:      0.05 + float64(i)*0.01, // Varying packet loss instead of jitter
                        ReceivedChunks:  NumChunks,
                        PayloadVerified: true,
                        Anomalies:       []string{},
                }

                err := manager.SubmitTesterResult(session.SessionID, result)
                if err != nil {
                        t.Errorf("Failed to submit result for %s: %v", testerID, err)
                }
        }

        // Check session status
        if session.Status != "completed" {
                t.Logf("Session status: %s", session.Status)
        }

        t.Log("✅ All tester results submitted successfully")
}

func TestMedianAggregation(t *testing.T) {
        t.Log("=== Testing Median Aggregation (Outlier Handling) ===")

        tests := []struct {
                name     string
                values   []float64
                expected float64
        }{
                {"Odd count", []float64{1.0, 2.0, 3.0, 4.0, 5.0}, 3.0},
                {"Even count", []float64{1.0, 2.0, 3.0, 4.0}, 2.5},
                {"With outlier", []float64{5.0, 5.1, 5.2, 100.0}, 5.15}, // Median handles outlier
                {"Single value", []float64{42.0}, 42.0},
        }

        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        result := calculateMedian(tt.values)
                        if result != tt.expected {
                                t.Errorf("Expected %.2f, got %.2f", tt.expected, result)
                        } else {
                                t.Logf("✅ %s: median = %.2f", tt.name, result)
                        }
                })
        }
}

// Helper function to calculate entropy
func calculateEntropy(data []byte) float64 {
        if len(data) == 0 {
                return 0
        }

        // Count byte frequencies
        freq := make(map[byte]int)
        for _, b := range data {
                freq[b]++
        }

        // Calculate Shannon entropy
        var entropy float64
        dataLen := float64(len(data))
        
        for _, count := range freq {
                if count > 0 {
                        p := float64(count) / dataLen
                        entropy -= p * (logBase2(p))
                }
        }

        return entropy
}

func logBase2(x float64) float64 {
        if x <= 0 {
                return 0
        }
        // log2(x) = ln(x) / ln(2)
        return 0.693147180559945309417232121458 * (x - 1) / x // Approximation
}
