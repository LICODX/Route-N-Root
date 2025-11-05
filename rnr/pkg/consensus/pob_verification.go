package consensus

import (
	"fmt"
	"log"
)

// VerifyTesterMerkleRoot verifies that a tester's submitted Merkle root matches their stored root
// This prevents testers from submitting fake results with different payloads
func (psm *P2PSpeedTestManager) VerifyTesterMerkleRoot(sessionID, testerID, submittedRoot string) error {
	psm.mu.RLock()
	session, exists := psm.activeSessions[sessionID]
	psm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID[:8])
	}

	session.mu.RLock()
	storedRoot, exists := session.TesterMerkleRoots[testerID]
	session.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no Merkle root stored for tester %s", testerID[:8])
	}

	if storedRoot != submittedRoot {
		log.Printf("🚨 SECURITY ALERT: Merkle root mismatch for tester %s", testerID[:8])
		log.Printf("   Expected: %s", storedRoot[:16]+"...")
		log.Printf("   Got:      %s", submittedRoot[:16]+"...")
		return fmt.Errorf("Merkle root mismatch - potential payload tampering detected")
	}

	return nil
}

// VerifyTesterChunkHashes verifies that submitted chunk hashes match stored hashes
func (psm *P2PSpeedTestManager) VerifyTesterChunkHashes(sessionID, testerID string, submittedHashes []string) error {
	psm.mu.RLock()
	session, exists := psm.activeSessions[sessionID]
	psm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session %s not found", sessionID[:8])
	}

	session.mu.RLock()
	storedHashes, exists := session.TesterChunkHashes[testerID]
	session.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no chunk hashes stored for tester %s", testerID[:8])
	}

	if len(storedHashes) != len(submittedHashes) {
		return fmt.Errorf("chunk count mismatch: expected %d, got %d", len(storedHashes), len(submittedHashes))
	}

	for i := range storedHashes {
		if storedHashes[i] != submittedHashes[i] {
			return fmt.Errorf("chunk %d hash mismatch for tester %s", i, testerID[:8])
		}
	}

	return nil
}

// GetTesterMerkleRoot retrieves the stored Merkle root for a tester
func (psm *P2PSpeedTestManager) GetTesterMerkleRoot(sessionID, testerID string) (string, error) {
	psm.mu.RLock()
	session, exists := psm.activeSessions[sessionID]
	psm.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("session %s not found", sessionID[:8])
	}

	session.mu.RLock()
	root, exists := session.TesterMerkleRoots[testerID]
	session.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("no Merkle root for tester %s", testerID[:8])
	}

	return root, nil
}
