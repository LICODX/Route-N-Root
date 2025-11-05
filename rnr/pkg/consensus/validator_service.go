package consensus

import (
        "crypto/ecdsa"
        "crypto/ed25519"
        "crypto/rand"
        "encoding/hex"
        "fmt"
        "log"
        "math/big"
        "sort"
        "time"

        "rnr-blockchain/pkg/blockchain"
        "rnr-blockchain/pkg/core"
        "rnr-blockchain/pkg/network"
)

type ValidatorService struct {
        validatorID     string
        privateKey      *ecdsa.PrivateKey
        publicKey       *ecdsa.PublicKey
        vrfSystem       *SecureVRFSystem
        votingManager   *VotingManager
        pobManager      *PoBTestManager
        p2pSpeedTestMgr *P2PSpeedTestManager
        finalityTracker *FinalityTracker
        retargetMgr     *PoBRetargetManager // Whitepaper Bab 9.1-9.2: PoB difficulty retargeting
        blockchain      *blockchain.Blockchain
        state           *blockchain.State
        mempool         *blockchain.Mempool
        poh             *ProofOfHistory
        p2pNetwork      *network.P2PNetwork // P2P network for speed tests
}

func NewValidatorService(
        validatorID string,
        privateKey *ecdsa.PrivateKey,
        blockchain *blockchain.Blockchain,
        state *blockchain.State,
        mempool *blockchain.Mempool,
        poh *ProofOfHistory,
) (*ValidatorService, error) {
        vrfSys, err := NewSecureVRFSystem()
        if err != nil {
                return nil, fmt.Errorf("failed to create VRF system: %w", err)
        }

        // Initialize voting manager with database for vote persistence
        votingMgr := NewVotingManager(state.GetDB())
        votingMgr.SetState(state)

        vs := &ValidatorService{
                validatorID:     validatorID,
                privateKey:      privateKey,
                publicKey:       &privateKey.PublicKey,
                vrfSystem:       vrfSys,
                votingManager:   votingMgr,
                pobManager:      NewPoBTestManager(),
                p2pSpeedTestMgr: NewP2PSpeedTestManager(),
                finalityTracker: NewFinalityTracker(),
                retargetMgr:     NewPoBRetargetManager(), // Whitepaper Bab 9.1-9.2
                blockchain:      blockchain,
                state:           state,
                mempool:         mempool,
                poh:             poh,
        }

        // SECURITY FIX: Store VRF public key in validator info for on-chain verification
        validatorInfo, err := state.GetValidator(validatorID)
        shortID := validatorID
        if len(validatorID) > 8 {
                shortID = validatorID[:8]
        }
        if err == nil && validatorInfo != nil {
                // Update validator with VRF public key
                validatorInfo.VRFPublicKey = vrfSys.GetPublicKey()
                state.UpdateValidator(validatorInfo)
                log.Printf("✅ Stored VRF public key for validator %s", shortID)
        } else {
                log.Printf("⚠️  Could not store VRF public key: validator %s not found in state", shortID)
        }

        return vs, nil
}

func (vs *ValidatorService) IsProposer(blockHeight uint64) (bool, string, error) {
        latestBlock := vs.blockchain.GetLatestBlock()
        prevBlockHash, err := latestBlock.Hash()
        if err != nil {
                return false, "", err
        }

        activeValidators := vs.state.GetActiveValidators()
        if len(activeValidators) == 0 {
                return false, "", fmt.Errorf("no active validators")
        }

        validatorKeys := make(map[string][]byte)
        pobScores := make(map[string]float64)
        eligibleValidators := make([]string, 0)
        
        for _, vid := range activeValidators {
                validatorInfo, err := vs.state.GetValidator(vid)
                if err == nil && validatorInfo != nil {
                        if !validatorInfo.IsSuspended {
                                validatorKeys[vid] = validatorInfo.PublicKey
                                pobScores[vid] = validatorInfo.PoBScore
                                eligibleValidators = append(eligibleValidators, vid)
                        }
                }
        }

        if len(eligibleValidators) == 0 {
                return false, "", fmt.Errorf("no eligible validators (all suspended)")
        }

        seed := GenerateProposerSeed(blockHeight, prevBlockHash)
        proposerID, err := PoBWeightedSelectProposer(seed, eligibleValidators, validatorKeys, pobScores)
        if err != nil {
                return false, "", err
        }

        return proposerID == vs.validatorID, proposerID, nil
}

func (vs *ValidatorService) ProposeBlock() (*core.Block, error) {
        log.Println("🔨 Proposing new block...")

        latestBlock := vs.blockchain.GetLatestBlock()
        prevBlockHash, err := latestBlock.Hash()
        if err != nil {
                return nil, fmt.Errorf("failed to get previous block hash: %w", err)
        }

        validatorInfo, _ := vs.state.GetValidator(vs.validatorID)
        uploadBandwidth := core.MinUploadBandwidth // Default 8 MB/s
        pobScore := 1.0
        if validatorInfo != nil {
                if validatorInfo.UploadBandwidth > 0 {
                        uploadBandwidth = validatorInfo.UploadBandwidth
                }
                pobScore = validatorInfo.PoBScore
        }

        // WHITEPAPER COMPLIANCE: Dynamic Block Capacity based on upload bandwidth
        // Formula: 0.30 × Upload_Validator (MB/s) × 10 detik
        maxBlockCapacityBytes := vs.calculateDynamicBlockCapacity(uploadBandwidth)
        
        // Select transactions up to capacity limit (in bytes, not count!)
        transactions := vs.mempool.GetTransactionsBySize(maxBlockCapacityBytes)
        
        filteredTxs := vs.selectAndValidateTransactions(transactions)

        log.Printf("📊 Dynamic Block Capacity: %.2f MB (%.0f bytes) based on Upload: %.2f MB/s", 
                float64(maxBlockCapacityBytes)/(1024*1024), float64(maxBlockCapacityBytes), uploadBandwidth)
        log.Printf("   Selected %d transactions (PoB score: %.2f)", len(filteredTxs), pobScore)

        merkleRoot, err := vs.calculateMerkleRoot(filteredTxs)
        if err != nil {
                return nil, fmt.Errorf("failed to calculate merkle root: %w", err)
        }

        pohSequence := vs.poh.GetSequence()
        vs.poh.Update(pohSequence)

        // SECURITY: Generate VRF proof for proposer selection verification
        blockHeight := latestBlock.Header.Height + 1
        vrfInput := []byte(fmt.Sprintf("block_%d", blockHeight))
        vrfResult, err := vs.vrfSystem.Generate(vrfInput)
        if err != nil {
                return nil, fmt.Errorf("failed to generate VRF proof: %w", err)
        }

        log.Printf("🔐 Generated VRF proof for block #%d", blockHeight)
        log.Printf("   VRF Value: %x...", vrfResult.Value[:8])
        log.Printf("   VRF Proof: %x...", vrfResult.Proof[:8])

        // Whitepaper Bab 4.3: Calculate PoB weight for fork resolution
        // Convert PoB score (0.0-1.0) to uint64 weight (0-1000)
        pobWeight := uint64(pobScore * 1000)
        
        header := &core.BlockHeader{
                Version:       1,
                PrevBlockHash: prevBlockHash,
                MerkleRoot:    merkleRoot,
                Timestamp:     time.Now(),
                Nonce:         0,
                Difficulty:    vs.blockchain.Difficulty,
                Height:        blockHeight,
                PoBScore:      pobScore,
                PoBWeight:     pobWeight,        // Whitepaper Bab 4.3: For cumulative difficulty calculation
                VRFProof:      vrfResult.Proof,  // SECURITY: Include VRF proof in header
                VRFOutput:     vrfResult.Value,  // VRF output for randomness
        }

        block := &core.Block{
                Header:       header,
                Transactions: filteredTxs,
                ProposerID:   vs.validatorID,
                PoHSequence:  pohSequence,
                Signature:    []byte{},
                VRFProof:     vrfResult.Proof,  // SECURITY: Duplicate for easy access
        }

        signature, err := vs.signBlock(block)
        if err != nil {
                return nil, fmt.Errorf("failed to sign block: %w", err)
        }
        block.Signature = signature

        blockHash, _ := block.Hash()
        _, err = vs.votingManager.StartVotingSession(blockHash, len(vs.state.GetActiveValidators()))
        if err != nil {
                return nil, fmt.Errorf("failed to start voting session: %w", err)
        }

        log.Printf("✅ Block #%d proposed with %d transactions", header.Height, len(filteredTxs))
        return block, nil
}

func (vs *ValidatorService) VoteOnBlock(block *core.Block) error {
        if err := vs.validateBlock(block); err != nil {
                log.Printf("❌ Block validation failed: %v", err)
                return fmt.Errorf("block validation failed: %w", err)
        }

        blockHash, err := block.Hash()
        if err != nil {
                return err
        }

        signature, err := SignVote(blockHash, vs.privateKey)
        if err != nil {
                return fmt.Errorf("failed to sign vote: %w", err)
        }

        err = vs.votingManager.CastVote(blockHash, vs.validatorID, signature)
        if err != nil {
                return fmt.Errorf("failed to cast vote: %w", err)
        }

        log.Printf("✅ Voted on block #%d (hash: %s)", block.Header.Height, hex.EncodeToString(blockHash)[:8])

        isFinalized, voteCount, _ := vs.votingManager.CheckFinality(blockHash)
        if isFinalized {
                vs.finalizeBlock(block)
                log.Printf("🎉 Block #%d finalized with %d votes!", block.Header.Height, voteCount)
        }

        return nil
}

func (vs *ValidatorService) finalizeBlock(block *core.Block) error {
        blockHash, _ := block.Hash()
        
        if err := vs.blockchain.AddBlock(block); err != nil {
                return fmt.Errorf("failed to add block to blockchain: %w", err)
        }

        for _, tx := range block.Transactions {
                vs.mempool.RemoveTransaction(tx.ID)
                vs.applyTransaction(tx)
        }

        vs.distributeBlockRewards(block)

        vs.finalityTracker.MarkFinalized(blockHash)

        // Whitepaper Bab 9.1: Record validator count for retargeting calculation
        activeValidators := vs.state.GetActiveValidators()
        vs.retargetMgr.RecordValidatorCount(len(activeValidators))
        
        // Whitepaper Bab 9.2: Adjust difficulty every 50 blocks
        if err := vs.retargetMgr.AdjustDifficulty(block.Header.Height); err != nil {
                log.Printf("⚠️  Failed to adjust PoB difficulty: %v", err)
        }

        return nil
}

func (vs *ValidatorService) distributeBlockRewards(block *core.Block) {
        blockReward := vs.calculateBlockReward(block.Header.Height)
        
        proposerReward := new(big.Int).Mul(blockReward, core.ProposerRewardPercentage)
        proposerReward = proposerReward.Div(proposerReward, big.NewInt(100))
        
        pobReward := new(big.Int).Mul(blockReward, core.PoBContributorPercentage)
        pobReward = pobReward.Div(pobReward, big.NewInt(100))
        
        proposerAccount, _ := vs.state.GetAccount(block.ProposerID)
        proposerAccount.Balance = new(big.Int).Add(proposerAccount.Balance, proposerReward)
        vs.state.UpdateAccount(proposerAccount)
        
        log.Printf("💰 Block Reward Distributed: Proposer=%s got %s RNR", 
                block.ProposerID[:12], proposerReward.String())

        activeValidators := vs.state.GetActiveValidators()
        validatorInfos := make(map[string]*core.ValidatorInfo)
        for _, vid := range activeValidators {
                if info, err := vs.state.GetValidator(vid); err == nil && info != nil {
                        if !info.IsSuspended {
                                validatorInfos[vid] = info
                        }
                }
        }

        groups := GroupValidatorsByNetwork(validatorInfos)
        pobRewards := DistributePoBRewardFairly(pobReward, groups, core.MaxPoBContributors)

        totalDistributed := big.NewInt(0)
        suspendedCount := 0
        for validatorID, reward := range pobRewards {
                if reward.Cmp(big.NewInt(0)) > 0 {
                        validatorInfo, _ := vs.state.GetValidator(validatorID)
                        if validatorInfo != nil && validatorInfo.IsSuspended {
                                suspendedCount++
                                continue
                        }
                        
                        account, _ := vs.state.GetAccount(validatorID)
                        account.Balance = new(big.Int).Add(account.Balance, reward)
                        vs.state.UpdateAccount(account)
                        totalDistributed = new(big.Int).Add(totalDistributed, reward)
                }
        }

        if len(groups) > 0 {
                log.Printf("📊 PoB Rewards: %d network groups, %s RNR total distributed", 
                        len(groups), totalDistributed.String())
        }
}

func (vs *ValidatorService) calculateBlockReward(blockHeight uint64) *big.Int {
        reductionCount := new(big.Int).Div(
                big.NewInt(int64(blockHeight)), 
                core.BlockReductionInterval,
        )
        
        reduction := new(big.Int).Mul(reductionCount, core.BlockRewardReduction)
        reward := new(big.Int).Sub(core.InitialBlockReward, reduction)
        
        if reward.Cmp(core.MinBlockReward) < 0 {
                reward = core.MinBlockReward
        }
        
        return reward
}

func (vs *ValidatorService) validateBlock(block *core.Block) error {
        if err := vs.blockchain.VerifyBlock(block); err != nil {
                return err
        }

        if !vs.poh.VerifySequence(block.PoHSequence) {
                return fmt.Errorf("invalid PoH sequence")
        }

        merkleRoot, err := vs.calculateMerkleRoot(block.Transactions)
        if err != nil {
                return err
        }
        if hex.EncodeToString(merkleRoot) != hex.EncodeToString(block.Header.MerkleRoot) {
                return fmt.Errorf("merkle root mismatch")
        }

        // Anti-spam: Limit new addresses per block (prevent wallet creation spam attacks)
        newAddresses := make(map[string]bool)
        for _, tx := range block.Transactions {
                // Check if sender is a new address
                if !vs.state.AccountExists(tx.From) {
                        newAddresses[tx.From] = true
                }
                // Check if recipient is a new address
                if !vs.state.AccountExists(tx.To) {
                        newAddresses[tx.To] = true
                }
        }

        if len(newAddresses) > core.MaxNewAddressesPerBlock {
                return fmt.Errorf("block contains %d new addresses, maximum allowed is %d (anti-spam protection)", 
                        len(newAddresses), core.MaxNewAddressesPerBlock)
        }

        // SECURITY FIX: Verify VRF proof for proposer selection
        if block.VRFProof == nil || len(block.VRFProof) == 0 {
                return fmt.Errorf("block missing VRF proof - invalid proposer selection")
        }

        // Get all validators for VRF verification
        activeValidators := vs.state.GetActiveValidators()
        validators := make([]*core.ValidatorInfo, 0, len(activeValidators))
        for _, vid := range activeValidators {
                if validatorInfo, err := vs.state.GetValidator(vid); err == nil && validatorInfo != nil {
                        validators = append(validators, validatorInfo)
                }
        }

        // Verify VRF proof on-chain
        if err := ValidateVRFProofOnChain(block, validators, vs.vrfSystem); err != nil {
                log.Printf("❌ VRF proof verification failed: %v", err)
                return fmt.Errorf("VRF proof verification failed: %w", err)
        }

        log.Printf("✅ VRF proof verified for block #%d from proposer %s", 
                block.Header.Height, block.ProposerID[:8])

        return nil
}

func (vs *ValidatorService) selectAndValidateTransactions(txs []*core.Transaction) []*core.Transaction {
        valid := make([]*core.Transaction, 0)

        sort.Slice(txs, func(i, j int) bool {
                return txs[i].Fee.Cmp(txs[j].Fee) > 0
        })

        for _, tx := range txs {
                if vs.validateTransaction(tx) {
                        valid = append(valid, tx)
                }
        }

        return valid
}

func (vs *ValidatorService) validateTransaction(tx *core.Transaction) bool {
        account, err := vs.state.GetAccount(tx.From)
        if err != nil {
                return false
        }

        if account.Nonce != tx.Nonce {
                return false
        }

        totalCost := new(big.Int).Add(tx.Amount, tx.Fee)
        if account.Balance.Cmp(totalCost) < 0 {
                return false
        }

        if len(tx.Signature) > 0 {
                txHash, err := tx.Hash()
                if err != nil {
                        return false
                }

                if len(tx.Signature) < 32 {
                        return false
                }

                r := new(big.Int).SetBytes(tx.Signature[:len(tx.Signature)/2])
                s := new(big.Int).SetBytes(tx.Signature[len(tx.Signature)/2:])

                validatorInfo, err := vs.state.GetValidator(tx.From)
                if err != nil {
                        return false
                }

                pubKey, err := DecodeECDSAPublicKey(validatorInfo.PublicKey)
                if err != nil {
                        return false
                }

                if !ecdsa.Verify(pubKey, txHash, r, s) {
                        return false
                }
        }

        return true
}

func (vs *ValidatorService) applyTransaction(tx *core.Transaction) {
        fromAccount, _ := vs.state.GetAccount(tx.From)
        toAccount, _ := vs.state.GetAccount(tx.To)

        // Whitepaper Bab 7.3: Base Fee Burning (EIP-1559 style)
        // Base Fee = 0.00000001% of transaction value = amount × 0.0000000001
        baseFee := new(big.Int).Mul(tx.Amount, big.NewInt(1))        // amount × 1
        baseFee = baseFee.Div(baseFee, big.NewInt(10000000000))      // ÷ 10^10 = 0.00000001%
        
        // Priority fee goes to proposer (handled in distributeBlockRewards)
        priorityFee := new(big.Int).Sub(tx.Fee, baseFee)
        if priorityFee.Sign() < 0 {
                priorityFee = big.NewInt(0)
        }

        // Total cost: amount + priority fee + base fee
        totalCost := new(big.Int).Add(tx.Amount, tx.Fee)
        fromAccount.Balance.Sub(fromAccount.Balance, totalCost)
        fromAccount.Nonce++

        // Recipient gets the amount (not including any fees)
        toAccount.Balance.Add(toAccount.Balance, tx.Amount)

        // Base fee is BURNED (not added to anyone's balance)
        // This is deflationary mechanism as per whitepaper
        if baseFee.Cmp(big.NewInt(0)) > 0 {
                log.Printf("🔥 Base fee burned: %s RNR from tx %s", baseFee.String(), tx.ID[:16])
        }

        vs.state.UpdateAccount(fromAccount)
        vs.state.UpdateAccount(toAccount)
}

func (vs *ValidatorService) calculateMerkleRoot(txs []*core.Transaction) ([]byte, error) {
        hashes := make([][]byte, len(txs))
        for i, tx := range txs {
                hash, err := tx.Hash()
                if err != nil {
                        return nil, err
                }
                hashes[i] = hash
        }
        return blockchain.ComputeMerkleRoot(hashes)
}

func (vs *ValidatorService) signBlock(block *core.Block) ([]byte, error) {
        hash, err := block.Hash()
        if err != nil {
                return nil, err
        }
        r, s, err := ecdsa.Sign(rand.Reader, vs.privateKey, hash)
        if err != nil {
                return nil, err
        }
        return append(r.Bytes(), s.Bytes()...), nil
}

func (vs *ValidatorService) RunPoBTest(candidateID string) (float64, error) {
        latestBlock := vs.blockchain.GetLatestBlock()
        blockHash, _ := latestBlock.Hash()

        activeValidators := vs.state.GetActiveValidators()
        testers, err := SecureSelectTestCommittee(candidateID, blockHash, activeValidators, core.MaxTestCommitteeSize, vs.vrfSystem)
        if err != nil {
                return 0, err
        }

        session, err := vs.pobManager.InitiateTest(candidateID, testers)
        if err != nil {
                return 0, err
        }

        log.Printf("🧪 Starting PoB test for candidate %s with %d testers", candidateID[:8], len(testers))

        for _, testerID := range testers {
                go func(tid string) {
                        result, err := ConductBandwidthTest(candidateID+":8080", session.TestData, 30*time.Second)
                        if err != nil {
                                log.Printf("⚠️  PoB test failed for tester %s: %v", tid[:8], err)
                                return
                        }
                        session.Results[tid] = result
                        log.Printf("✅ PoB test completed by %s: upload=%.2f MB/s, latency=%.2f ms, packet_loss=%.2f%%", 
                                tid[:8], result.UploadBandwidth, result.Latency, result.PacketLoss)
                }(testerID)
        }

        time.Sleep(35 * time.Second)

        score, err := vs.pobManager.AggregateResults(candidateID)
        if err != nil {
                return 0, err
        }

        validatorInfo, _ := vs.state.GetValidator(candidateID)
        if validatorInfo != nil {
                validatorInfo.PoBScore = score
                validatorInfo.LastPoBTest = time.Now()
                vs.state.UpdateValidator(validatorInfo)
        }

        log.Printf("📊 PoB test complete for %s: score = %.3f", candidateID[:8], score)
        return score, nil
}

func (vs *ValidatorService) GetVRFPublicKey() ed25519.PublicKey {
        return vs.vrfSystem.GetPublicKey()
}

// calculateDynamicBlockCapacity implements the whitepaper formula (Bab 4.2):
// Kapasitas Blok Maks = 0.30 × Upload_Validator (MB/s) × 10 detik
// This prevents network congestion by ensuring blocks match proposer's upload capability
func (vs *ValidatorService) calculateDynamicBlockCapacity(uploadBandwidthMBps float64) int {
        // If no bandwidth data, use minimum safe bandwidth (Whitepaper: 7 MB/s)
        if uploadBandwidthMBps <= 0 {
                uploadBandwidthMBps = core.MinUploadBandwidth // 7 MB/s
        }

        // Whitepaper formula: 0.30 × Upload × 10 seconds
        // DynamicBlockCapacityRatio = 0.30
        // PropagationPhase = 10 seconds
        propagationTimeSeconds := core.PropagationPhase.Seconds() // 10.0
        maxCapacityMB := core.DynamicBlockCapacityRatio * uploadBandwidthMBps * propagationTimeSeconds
        
        // Convert MB to bytes
        maxCapacityBytes := int(maxCapacityMB * 1024 * 1024)
        
        // Apply reasonable bounds (min 5 MB, max 300 MB per block)
        minCapacityBytes := 5 * 1024 * 1024   // 5 MB minimum
        maxCapacityBytes_limit := 300 * 1024 * 1024 // 300 MB maximum
        
        if maxCapacityBytes < minCapacityBytes {
                maxCapacityBytes = minCapacityBytes
        }
        
        if maxCapacityBytes > maxCapacityBytes_limit {
                maxCapacityBytes = maxCapacityBytes_limit
        }

        return maxCapacityBytes
}
