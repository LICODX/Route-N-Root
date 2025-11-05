package consensus

import (
        "bytes"
        "encoding/hex"
        "fmt"
        "log"

        "rnr-blockchain/pkg/core"
)

// VerifyBlockVRFProof verifies that the VRF proof in a block is valid
// This ensures the proposer was legitimately selected via VRF
func VerifyBlockVRFProof(block *core.Block, vrfPublicKey []byte, input []byte, vrfOutput []byte) error {
        if block.VRFProof == nil || len(block.VRFProof) == 0 {
                return fmt.Errorf("block missing VRF proof")
        }

        if vrfOutput == nil || len(vrfOutput) == 0 {
                return fmt.Errorf("block missing VRF output")
        }

        // SECURITY FIX: Use proper VRF verification from Yahoo library
        // This independently verifies VRF output correctness - prevents proposer manipulation
        isValid := VerifyVRFWithPublicKey(input, vrfOutput, block.VRFProof, vrfPublicKey)
        if !isValid {
                return fmt.Errorf("invalid VRF proof - verification failed")
        }

        // Verify VRF output in header matches the one we're verifying
        if block.Header != nil && block.Header.VRFOutput != nil {
                if !bytes.Equal(block.Header.VRFOutput, vrfOutput) {
                        return fmt.Errorf("VRF output mismatch in block header")
                }
        }

        log.Printf("✅ VRF proof verified for block proposer %s", block.ProposerID[:8])
        return nil
}

// VerifyVRFProposerSelection verifies that proposer was correctly selected via VRF
// Checks that VRF output falls within proposer's selection range based on PoB score
func VerifyVRFProposerSelection(
        block *core.Block,
        validators []*core.ValidatorInfo,
        vrfSystem *SecureVRFSystem,
) error {
        
        if block.VRFProof == nil {
                return fmt.Errorf("block missing VRF proof")
        }

        // Find proposer in validator set
        var proposer *core.ValidatorInfo
        for _, v := range validators {
                if v.ID == block.ProposerID {
                        proposer = v
                        break
                }
        }

        if proposer == nil {
                return fmt.Errorf("proposer %s not found in validator set", block.ProposerID[:8])
        }

        // Verify proposer has sufficient PoB score
        if proposer.PoBScore < 0.5 {
                return fmt.Errorf("proposer PoB score too low: %.3f", proposer.PoBScore)
        }

        // Calculate total weighted stake
        totalWeight := 0.0
        for _, v := range validators {
                if v.IsActive && !v.IsSuspended {
                        totalWeight += v.PoBScore
                }
        }

        // Verify VRF output corresponds to valid selection
        vrfOutput := block.Header.VRFOutput
        if vrfOutput == nil {
                vrfOutput = block.VRFProof[:32]
        }

        // Convert VRF output to selection value [0, 1)
        selectionValue := float64(vrfOutput[0]) / 256.0

        // Calculate proposer's selection range
        cumulativeWeight := 0.0
        proposerStart := 0.0
        proposerEnd := 0.0

        for _, v := range validators {
                if !v.IsActive || v.IsSuspended {
                        continue
                }

                rangeStart := cumulativeWeight / totalWeight
                cumulativeWeight += v.PoBScore
                rangeEnd := cumulativeWeight / totalWeight

                if v.ID == block.ProposerID {
                        proposerStart = rangeStart
                        proposerEnd = rangeEnd
                        break
                }
        }

        // Verify selection value falls within proposer's range
        if selectionValue < proposerStart || selectionValue >= proposerEnd {
                return fmt.Errorf(
                        "VRF selection mismatch: value %.4f not in proposer range [%.4f, %.4f)",
                        selectionValue, proposerStart, proposerEnd,
                )
        }

        log.Printf("✅ VRF proposer selection verified:")
        log.Printf("   Proposer: %s", block.ProposerID[:8])
        log.Printf("   PoB Score: %.3f", proposer.PoBScore)
        log.Printf("   Selection value: %.4f in range [%.4f, %.4f)", 
                selectionValue, proposerStart, proposerEnd)

        return nil
}

// BroadcastVRFProof broadcasts VRF proof to all validators for verification
func BroadcastVRFProof(vrfProof []byte, vrfOutput []byte, proposerID string) {
        log.Printf("📡 Broadcasting VRF proof for proposer %s", proposerID[:8])
        log.Printf("   Proof: %s...", hex.EncodeToString(vrfProof[:16]))
        log.Printf("   Output: %s...", hex.EncodeToString(vrfOutput[:16]))
        
        // TODO: Integrate with P2P network to broadcast
        // For now, log the broadcast intent
}

// ValidateVRFProofOnChain validates VRF proof during block validation
// This is called by validators when receiving a new block
func ValidateVRFProofOnChain(
        block *core.Block,
        validators []*core.ValidatorInfo,
        vrfSystem *SecureVRFSystem,
) error {
        
        // Step 1: Verify VRF proof cryptographic validity
        // Use block height as VRF input for deterministic verification
        vrfInput := []byte(fmt.Sprintf("block_%d", block.Header.Height))
        
        // SECURITY FIX: Get proposer's correct VRF public key
        var proposerPubKey []byte
        for _, v := range validators {
                if v.ID == block.ProposerID {
                        // Use dedicated VRF public key field
                        if v.VRFPublicKey != nil && len(v.VRFPublicKey) > 0 {
                                proposerPubKey = v.VRFPublicKey
                        } else {
                                return fmt.Errorf("proposer %s missing valid VRF public key", block.ProposerID[:8])
                        }
                        break
                }
        }

        if proposerPubKey == nil || len(proposerPubKey) == 0 {
                return fmt.Errorf("proposer VRF public key not found for %s", block.ProposerID[:8])
        }

        // SECURITY FIX: Verify VRF proof with both input AND output
        vrfOutput := block.Header.VRFOutput
        
        if vrfOutput == nil || len(vrfOutput) == 0 {
                return fmt.Errorf("block missing VRF output")
        }

        if err := VerifyBlockVRFProof(block, proposerPubKey, vrfInput, vrfOutput); err != nil {
                return fmt.Errorf("VRF proof verification failed: %w", err)
        }

        // Step 2: Verify proposer selection is correct based on VRF output
        if err := VerifyVRFProposerSelection(block, validators, vrfSystem); err != nil {
                return fmt.Errorf("VRF proposer selection verification failed: %w", err)
        }

        log.Printf("✅ VRF proof validated on-chain for block %d", block.Header.Height)
        return nil
}
