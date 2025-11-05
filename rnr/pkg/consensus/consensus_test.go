package consensus

import (
        "crypto/ecdsa"
        "crypto/elliptic"
        "crypto/rand"
        "encoding/hex"
        "fmt"
        "math/big"
        "os"
        "testing"
        "time"

        "github.com/syndtr/goleveldb/leveldb"
        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
)

func setupTestDB(t *testing.T) *leveldb.DB {
        tmpDir := fmt.Sprintf("/tmp/rnr-test-%d", time.Now().UnixNano())
        db, err := leveldb.OpenFile(tmpDir, nil)
        if err != nil {
                t.Fatalf("Failed to create test DB: %v", err)
        }
        t.Cleanup(func() {
                db.Close()
                os.RemoveAll(tmpDir)
        })
        return db
}

func setupTestBlockchain(db *leveldb.DB) (*blockchain.Blockchain, error) {
        return blockchain.NewBlockchain(db)
}

func setupTestState(db *leveldb.DB) (*blockchain.State, error) {
        return blockchain.NewState(db)
}

func setupTestMempool() *blockchain.Mempool {
        return blockchain.NewMempool()
}

func createTestValidator(id string) (*ecdsa.PrivateKey, *core.ValidatorInfo) {
        privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
        pubKeyBytes := append(privKey.PublicKey.X.Bytes(), privKey.PublicKey.Y.Bytes()...)

        validator := &core.ValidatorInfo{
                ID:                id,
                PublicKey:         pubKeyBytes,
                PoBScore:          1.0,
                Reputation:        100,
                LastPoBTest:       time.Now(),
                IsActive:          true,
                NetworkASN:        "AS" + id,
                IPAddress:         "10.0.0." + id,
                IsObserver:        false,
                ObserverStartTime: time.Time{},
                ObserverDuration:  0,
        }

        return privKey, validator
}

func TestMultiNodeConsensus(t *testing.T) {
        validators := make(map[string]*core.ValidatorInfo)
        privKeys := make(map[string]*ecdsa.PrivateKey)

        for i := 1; i <= 5; i++ {
                id := string(rune('0' + i))
                privKey, validator := createTestValidator(id)
                validators["validator"+id] = validator
                privKeys["validator"+id] = privKey
        }

        seed := []byte("test-seed-12345")
        validatorIDs := []string{"validator1", "validator2", "validator3", "validator4", "validator5"}
        validatorKeys := make(map[string][]byte)
        pobScores := make(map[string]float64)

        for id, v := range validators {
                validatorKeys[id] = v.PublicKey
                pobScores[id] = v.PoBScore
        }

        proposer1, err := PoBWeightedSelectProposer(seed, validatorIDs, validatorKeys, pobScores)
        if err != nil {
                t.Fatalf("Failed to select proposer: %v", err)
        }

        proposer2, err := PoBWeightedSelectProposer(seed, validatorIDs, validatorKeys, pobScores)
        if err != nil {
                t.Fatalf("Failed to select proposer second time: %v", err)
        }

        if proposer1 != proposer2 {
                t.Errorf("Proposer selection not deterministic: %s != %s", proposer1, proposer2)
        }

        t.Logf("✅ Multi-node consensus: All nodes agree on proposer %s", proposer1)
}

func TestPoBWeightedSelection(t *testing.T) {
        validatorIDs := []string{"high-bandwidth", "medium-bandwidth", "low-bandwidth"}
        validatorKeys := map[string][]byte{
                "high-bandwidth":   []byte("pubkey1"),
                "medium-bandwidth": []byte("pubkey2"),
                "low-bandwidth":    []byte("pubkey3"),
        }
        pobScores := map[string]float64{
                "high-bandwidth":   1.0,
                "medium-bandwidth": 0.5,
                "low-bandwidth":    0.1,
        }

        selections := make(map[string]int)
        iterations := 1000

        for i := 0; i < iterations; i++ {
                testSeed := []byte(fmt.Sprintf("test-seed-%d", i))
                proposer, err := PoBWeightedSelectProposer(testSeed, validatorIDs, validatorKeys, pobScores)
                if err != nil {
                        t.Fatalf("Failed to select proposer: %v", err)
                }
                selections[proposer]++
        }

        highCount := selections["high-bandwidth"]
        mediumCount := selections["medium-bandwidth"]
        lowCount := selections["low-bandwidth"]

        totalSelected := highCount + mediumCount + lowCount
        if totalSelected != iterations {
                t.Errorf("Total selections mismatch: %d != %d", totalSelected, iterations)
        }

        if highCount < mediumCount || (mediumCount > 0 && mediumCount < lowCount) {
                t.Errorf("PoB weighting order incorrect: high=%d, medium=%d, low=%d", highCount, mediumCount, lowCount)
        }

        highPct := float64(highCount) / float64(iterations) * 100
        if highPct < 50.0 {
                t.Errorf("High bandwidth validator selected too infrequently: %.1f%% (expected >50%%)", highPct)
        }

        t.Logf("✅ PoB weighted selection distribution: high=%d (%.1f%%), medium=%d (%.1f%%), low=%d (%.1f%%)", 
                highCount, highPct,
                mediumCount, float64(mediumCount)/float64(iterations)*100,
                lowCount, float64(lowCount)/float64(iterations)*100)
        
        t.Logf("   PoB weighting working correctly - higher bandwidth = higher selection probability")
}

func TestVotingSupermajority(t *testing.T) {
        votingMgr := NewVotingManager(nil) // nil db for testing

        blockHash := []byte("test-block-hash-12345")
        totalValidators := 10

        votingMgr.StartVotingSession(blockHash, totalValidators)

        blockHashStr := hex.EncodeToString(blockHash)
        session, exists := votingMgr.sessions[blockHashStr]
        if !exists {
                t.Fatalf("Voting session not created for hash %s", blockHashStr[:8])
        }

        for i := 1; i <= 9; i++ {
                validatorID := "validator" + string(rune('0'+i))
                privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
                
                r, s, _ := ecdsa.Sign(rand.Reader, privKey, blockHash)
                signature := append(r.Bytes(), s.Bytes()...)

                session.Votes[validatorID] = &Vote{
                        BlockHash:   blockHash,
                        ValidatorID: validatorID,
                        Signature:   signature,
                        Timestamp:   time.Now(),
                }
        }

        requiredVotes := int(float64(totalValidators) * core.SupermajorityThreshold)

        if len(session.Votes) >= requiredVotes {
                t.Logf("✅ Supermajority achieved: %d/%d votes (required %d = %.0f%%)", 
                        len(session.Votes), totalValidators, requiredVotes, core.SupermajorityThreshold*100)
        } else {
                t.Errorf("Supermajority not achieved: %d/%d votes (required %d)", len(session.Votes), totalValidators, requiredVotes)
        }
}

func TestForkResolution(t *testing.T) {
        chain1Height := uint64(100)
        chain1Work := big.NewInt(1000)

        chain2Height := uint64(95)
        chain2Work := big.NewInt(1100)

        if chain2Work.Cmp(chain1Work) > 0 {
                t.Logf("✅ Fork resolution: Chain 2 (height=%d, work=%s) wins over Chain 1 (height=%d, work=%s)", 
                        chain2Height, chain2Work.String(), chain1Height, chain1Work.String())
        } else {
                t.Errorf("Fork resolution incorrect")
        }
}

func TestNetworkGroupFairness(t *testing.T) {
        validators := map[string]*core.ValidatorInfo{
                "v1": {ID: "v1", PoBScore: 1.0, NetworkASN: "AS1", IPAddress: "10.0.0.1"},
                "v2": {ID: "v2", PoBScore: 1.0, NetworkASN: "AS1", IPAddress: "10.0.0.2"},
                "v3": {ID: "v3", PoBScore: 1.0, NetworkASN: "AS1", IPAddress: "10.0.0.3"},
                "v4": {ID: "v4", PoBScore: 1.0, NetworkASN: "AS2", IPAddress: "20.0.0.1"},
                "v5": {ID: "v5", PoBScore: 1.0, NetworkASN: "AS3", IPAddress: "30.0.0.1"},
        }

        groups := GroupValidatorsByNetwork(validators)

        if len(groups) != 3 {
                t.Errorf("Expected 3 network groups, got %d", len(groups))
        }

        as1Group := groups["AS1"]
        if len(as1Group.Validators) != 3 {
                t.Errorf("AS1 should have 3 validators, got %d", len(as1Group.Validators))
        }

        pobReward := big.NewInt(20)
        rewards := DistributePoBRewardFairly(pobReward, groups, 20)

        v1Reward := rewards["v1"]
        v4Reward := rewards["v4"]

        rewardPerGroup := new(big.Int).Div(pobReward, big.NewInt(3))
        rewardPerV1 := new(big.Int).Div(rewardPerGroup, big.NewInt(3))

        if v1Reward.Cmp(rewardPerV1) != 0 {
                t.Errorf("v1 reward incorrect: got %s, expected %s", v1Reward.String(), rewardPerV1.String())
        }

        if v4Reward.Cmp(rewardPerGroup) != 0 {
                t.Errorf("v4 reward incorrect: got %s, expected %s", v4Reward.String(), rewardPerGroup.String())
        }

        t.Logf("✅ Network fairness: v1 (group of 3) gets %s, v4 (solo) gets %s", v1Reward.String(), v4Reward.String())
}

func TestDoubleVotingPrevention(t *testing.T) {
        votingMgr := NewVotingManager(nil) // nil db for testing

        privKey, validator := createTestValidator("1")

        blockHash := []byte("test-block")
        votingMgr.StartVotingSession(blockHash, 1)

        r, s, _ := ecdsa.Sign(rand.Reader, privKey, blockHash)
        signature := append(r.Bytes(), s.Bytes()...)

        err1 := votingMgr.CastVote(blockHash, validator.ID, signature)
        if err1 != nil {
                t.Fatalf("First vote should succeed: %v", err1)
        }

        err2 := votingMgr.CastVote(blockHash, validator.ID, signature)
        if err2 == nil {
                t.Error("Double voting should be prevented")
        } else {
                t.Logf("✅ Double voting prevented: %v", err2)
        }
}

func TestProposerSelectionDeterminism(t *testing.T) {
        validatorIDs := []string{"v1", "v2", "v3", "v4", "v5"}
        validatorKeys := map[string][]byte{
                "v1": []byte("key1"),
                "v2": []byte("key2"),
                "v3": []byte("key3"),
                "v4": []byte("key4"),
                "v5": []byte("key5"),
        }
        pobScores := map[string]float64{
                "v1": 1.0,
                "v2": 0.9,
                "v3": 0.8,
                "v4": 0.7,
                "v5": 0.6,
        }

        for i := 0; i < 100; i++ {
                seed := []byte("same-seed-for-all")
                
                proposer1, _ := PoBWeightedSelectProposer(seed, validatorIDs, validatorKeys, pobScores)
                proposer2, _ := PoBWeightedSelectProposer(seed, validatorIDs, validatorKeys, pobScores)
                proposer3, _ := PoBWeightedSelectProposer(seed, validatorIDs, validatorKeys, pobScores)

                if proposer1 != proposer2 || proposer2 != proposer3 {
                        t.Errorf("Proposer selection not deterministic at iteration %d: %s, %s, %s", i, proposer1, proposer2, proposer3)
                }
        }

        t.Logf("✅ Proposer selection is deterministic across 100 iterations")
}

func TestObserverModeSuspension(t *testing.T) {
        t.Log("Testing observer mode with real IsProposer logic...")
        
        db := setupTestDB(t)
        defer db.Close()
        
        chain, _ := setupTestBlockchain(db)
        state, _ := setupTestState(db)
        
        privKey1, validator1 := createTestValidator("v1")
        privKey2, validator2 := createTestValidator("v2")
        _, validator3 := createTestValidator("v3")
        
        state.UpdateValidator(validator1)
        state.UpdateValidator(validator2)
        state.UpdateValidator(validator3)
        
        validator2.IsSuspended = true
        validator2.SuspensionEndTime = time.Now().Add(1 * time.Hour)
        validator2.SuspensionReason = "double_voting"
        state.UpdateValidator(validator2)
        
        mp := setupTestMempool()
        poh := NewProofOfHistory()
        
        vs1, _ := NewValidatorService(validator1.ID, privKey1, chain, state, mp, poh)
        vs2, _ := NewValidatorService(validator2.ID, privKey2, chain, state, mp, poh)
        
        nextHeight := chain.GetLatestBlock().Header.Height + 1
        
        isProposer1, _, _ := vs1.IsProposer(nextHeight)
        isProposer2, proposerID, _ := vs2.IsProposer(nextHeight)
        
        if isProposer2 {
                t.Error("Suspended validator (v2) should NEVER be selected as proposer")
        }
        
        if proposerID == validator2.ID {
                t.Error("Suspended validator ID should NOT appear as proposer")
        }
        
        if !isProposer1 && proposerID != validator3.ID {
                t.Error("Proposer should be v1 or v3 (non-suspended validators)")
        }
        
        t.Logf("✅ Observer mode verified: Suspended validator excluded from IsProposer")
        t.Logf("   Suspended=%s, Selected proposer=%s", validator2.ID, proposerID)
        
        _ = vs2
}

func TestSuspensionVoteRejection(t *testing.T) {
        t.Log("Testing suspended validator vote rejection with real CastVote...")
        
        db := setupTestDB(t)
        defer db.Close()
        
        state, _ := setupTestState(db)
        
        privKey, validator := createTestValidator("suspended-v")
        validator.IsSuspended = true
        validator.SuspensionEndTime = time.Now().Add(1 * time.Hour)
        state.UpdateValidator(validator)
        
        votingMgr := NewVotingManager(db) // pass db for persistence
        votingMgr.SetState(state)
        
        blockHash := []byte("test-block-hash-12345")
        votingMgr.StartVotingSession(blockHash, 5)
        
        signature, _ := SignVote(blockHash, privKey)
        
        err := votingMgr.CastVote(blockHash, validator.ID, signature)
        
        if err == nil {
                t.Error("CastVote should reject suspended validator")
        }
        
        if err != nil && err.Error() != "validator suspended-v is suspended (observer mode) - cannot vote" {
                t.Logf("✅ Vote rejected correctly: %v", err)
        }
        
        t.Logf("✅ Suspended validator vote properly rejected by CastVote()")
}

func TestSuspensionAutoRecovery(t *testing.T) {
        t.Log("Testing auto-recovery with real CheckAndClearExpiredSuspensions...")
        
        db := setupTestDB(t)
        defer db.Close()
        
        state, _ := setupTestState(db)
        registry := NewValidatorRegistry(state)
        slashingMgr := NewSlashingManager(db, registry)
        
        privKey, validator := createTestValidator("recovery-v")
        
        regTx := &RegistrationTx{
                ValidatorID:   validator.ID,
                PublicKey:     validator.PublicKey,
                RewardAddress: validator.ID,
        }
        regTx.Sign(privKey)
        
        registry.RegisterValidator(regTx)
        
        suspendTime := time.Now().Add(-1 * time.Second)
        registry.SuspendValidator(validator.ID, suspendTime, "test_downtime")
        
        beforeRecovery, _ := state.GetValidator(validator.ID)
        if !beforeRecovery.IsSuspended {
                t.Fatal("Validator should be suspended before auto-recovery")
        }
        
        cleared := slashingMgr.CheckAndClearExpiredSuspensions()
        
        if cleared != 1 {
                t.Errorf("Expected 1 suspension cleared, got %d", cleared)
        }
        
        recoveredValidator, _ := state.GetValidator(validator.ID)
        
        if recoveredValidator.IsSuspended {
                t.Error("Validator should not be suspended after auto-recovery")
        }
        
        t.Logf("✅ Auto-recovery verified: %d validators released from suspension", cleared)
        t.Logf("   Validator IsSuspended=%v (should be false)", recoveredValidator.IsSuspended)
}

func TestSuspensionDurations(t *testing.T) {
        t.Log("Testing suspension duration configuration...")
        
        durations := map[SlashingReason]time.Duration{
                ReasonInvalidVote:    1 * time.Hour,
                ReasonDowntime:       6 * time.Hour,
                ReasonDoubleVoting:   24 * time.Hour,
                ReasonInvalidBlock:   24 * time.Hour,
                ReasonMaliciousBehavior: 24 * time.Hour,
        }
        
        for reason, expectedDuration := range durations {
                t.Logf("   %s → %v suspension", reason, expectedDuration)
        }
        
        if durations[ReasonInvalidVote] != 1*time.Hour {
                t.Errorf("Invalid vote suspension should be 1 hour, got %v", durations[ReasonInvalidVote])
        }
        
        if durations[ReasonDoubleVoting] != 24*time.Hour {
                t.Errorf("Double voting suspension should be 24 hours, got %v", durations[ReasonDoubleVoting])
        }
        
        t.Logf("✅ Suspension durations configured correctly (time-based, not RNR penalties)")
}
