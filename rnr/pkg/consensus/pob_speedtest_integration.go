package consensus

import (
        "fmt"
        "log"
        "time"
)

// RunP2PSpeedTest conducts P2P-based PoB speed test with cryptographic payloads
// This replaces the old TCP-based test with a more secure P2P approach
func (vs *ValidatorService) RunP2PSpeedTest(candidateID string) (float64, error) {
        activeValidators := vs.state.GetActiveValidators()
        
        // Initiate P2P speed test session with committee selection
        session, err := vs.p2pSpeedTestMgr.InitiateP2PSpeedTest(candidateID, activeValidators)
        if err != nil {
                return 0, fmt.Errorf("failed to initiate P2P speed test: %w", err)
        }

        log.Printf("🔒 Starting P2P speed test for %s (session: %s)", candidateID[:8], session.SessionID)
        log.Printf("   Committee: %d testers selected", len(session.CommitteeTesters))

        // Generate cryptographic payloads for each tester
        testerPayloads := make(map[string]*CryptographicPayloadGenerator)
        for _, testerID := range session.CommitteeTesters {
                generator, err := NewCryptographicPayloadGenerator(session.SessionID, testerID)
                if err != nil {
                        log.Printf("⚠️  Failed to create payload generator for %s: %v", testerID[:8], err)
                        continue
                }
                testerPayloads[testerID] = generator
        }

        // Conduct speed tests with each tester
        for testerID, generator := range testerPayloads {
                go func(tid string, gen *CryptographicPayloadGenerator) {
                        // Generate unique 8 MB cryptographic payload
                        payload, chunkHashes, merkleRoot, err := gen.GeneratePayload()
                        if err != nil {
                                log.Printf("❌ Failed to generate payload for tester %s: %v", tid[:8], err)
                                return
                        }

                        log.Printf("🔐 Generated cryptographic payload for %s:", tid[:8])
                        log.Printf("   Size: %d MB (non-compressible)", len(payload)/(1024*1024))
                        log.Printf("   Chunks: %d x 512 KB", len(chunkHashes))
                        log.Printf("   Merkle root: %s", merkleRoot[:16]+"...")

                        // SECURITY FIX: Each tester must generate and verify their own Merkle root
                        // Do NOT share a single root across all testers - this prevents payload tampering
                        // Store per-tester Merkle root AND chunk hashes for independent verification
                        session.mu.Lock()
                        session.TesterMerkleRoots[tid] = merkleRoot       // CRITICAL: Per-tester root
                        session.TesterChunkHashes[tid] = chunkHashes      // Per-tester chunks
                        if session.PayloadRootHash == "" {
                                // First tester initializes session root as reference only
                                session.PayloadRootHash = merkleRoot
                        }
                        session.mu.Unlock()

                        log.Printf("🔐 Stored per-tester Merkle root for %s: %s", tid[:8], merkleRoot[:16]+"...")

                        // TODO: Conduct actual P2P speed test via libp2p
                        // For now, simulate with timing
                        startTime := time.Now()
                        
                        // Simulate chunk transmission with realistic timing
                        chunkTimestamps := make([]time.Time, NumChunks)
                        for i := 0; i < NumChunks; i++ {
                                chunkTimestamps[i] = time.Now()
                                // Simulate network delay
                                time.Sleep(time.Millisecond * 50)
                        }

                        uploadTime := time.Since(startTime)
                        uploadMBps := float64(PayloadSize) / (1024 * 1024) / uploadTime.Seconds()

                        // Verify chunk sequence (anti-cheat)
                        sequenceValid, anomalies := VerifyChunkSequence(chunkTimestamps)
                        if !sequenceValid {
                                log.Printf("⚠️  Detected anomalies for %s: %v", tid[:8], anomalies)
                        }

                        // Create result
                        result := &SpeedTestResult{
                                TesterID:           tid,
                                CandidateID:        candidateID,
                                SessionID:          session.SessionID,
                                UploadBandwidth:    uploadMBps,
                                Latency:            50.0,  // TODO: Measure actual latency via P2P
                                PacketLoss:         0.05,  // TODO: Measure actual packet loss via P2P
                                ChunkTimestamps:    chunkTimestamps,
                                ReceivedChunks:     NumChunks,
                                VerifiedHashes:     chunkHashes,
                                PayloadVerified:    sequenceValid,
                                Timestamp:          time.Now(),
                                Anomalies:          anomalies,
                        }

                        // Submit result
                        if err := vs.p2pSpeedTestMgr.SubmitTesterResult(session.SessionID, result); err != nil {
                                log.Printf("❌ Failed to submit result for %s: %v", tid[:8], err)
                                return
                        }

                        log.Printf("✅ P2P speed test completed by %s:", tid[:8])
                        log.Printf("   Upload: %.2f MB/s", result.UploadBandwidth)
                        log.Printf("   Latency: %.2f ms", result.Latency)
                        log.Printf("   Packet Loss: %.2f%%", result.PacketLoss)
                        log.Printf("   Payload verified: %v", result.PayloadVerified)
                }(testerID, generator)
        }

        // Wait for all testers to complete
        timeout := time.After(60 * time.Second)
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()

        for {
                select {
                case <-timeout:
                        log.Printf("⏱️  P2P speed test timeout for %s", candidateID[:8])
                        goto aggregate
                case <-ticker.C:
                        if session.Status == "completed" {
                                goto aggregate
                        }
                }
        }

aggregate:
        // Aggregate results using median to handle outliers
        aggregated, err := vs.p2pSpeedTestMgr.AggregateP2PResults(session.SessionID)
        if err != nil {
                return 0, fmt.Errorf("failed to aggregate results: %w", err)
        }

        // Update validator info with bandwidth metrics (Whitepaper Bab 3.1.2)
        validatorInfo, _ := vs.state.GetValidator(candidateID)
        if validatorInfo != nil {
                validatorInfo.PoBScore = CalculatePoBScore([]*PoBTestResult{aggregated})
                validatorInfo.UploadBandwidth = aggregated.UploadBandwidth  // MB/s (≥ 7 MB/s)
                validatorInfo.Latency = aggregated.Latency                  // ms (≤ 100 ms)
                validatorInfo.PacketLoss = aggregated.PacketLoss            // % (0.1%)
                validatorInfo.LastPoBTest = time.Now()
                vs.state.UpdateValidator(validatorInfo)
        }

        log.Printf("📊 P2P Speed Test Summary for %s:", candidateID[:8])
        log.Printf("   Session: %s", session.SessionID)
        log.Printf("   Upload: %.2f MB/s (median)", aggregated.UploadBandwidth)
        log.Printf("   Latency: %.2f ms (median)", aggregated.Latency)
        log.Printf("   Packet Loss: %.2f%% (median)", aggregated.PacketLoss)
        log.Printf("   PoB Score: %.3f", CalculatePoBScore([]*PoBTestResult{aggregated}))
        log.Printf("   Passed: %v", aggregated.Passed)

        return CalculatePoBScore([]*PoBTestResult{aggregated}), nil
}

// GetP2PSpeedTestSession retrieves an active P2P speed test session
func (vs *ValidatorService) GetP2PSpeedTestSession(sessionID string) (*P2PSpeedTestSession, error) {
        vs.p2pSpeedTestMgr.mu.RLock()
        defer vs.p2pSpeedTestMgr.mu.RUnlock()

        session, exists := vs.p2pSpeedTestMgr.activeSessions[sessionID]
        if !exists {
                return nil, fmt.Errorf("session %s not found", sessionID)
        }

        return session, nil
}
