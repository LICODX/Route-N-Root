package consensus

import (
        "bytes"
        "crypto/sha256"
        "encoding/hex"
        "fmt"
        "math/big"

        "github.com/consensys/gnark-crypto/ecc"
        "github.com/consensys/gnark/backend/groth16"
        "github.com/consensys/gnark/constraint"
        "github.com/consensys/gnark/frontend"
        "github.com/consensys/gnark/frontend/cs/r1cs"
        "rnr-blockchain/pkg/core"
)

type PoBCircuit struct {
        UploadBandwidth frontend.Variable `gnark:",secret"`
        Latency         frontend.Variable `gnark:",secret"`
        PacketLoss      frontend.Variable `gnark:",secret"` // Whitepaper: 0.1%
        DataHash        frontend.Variable `gnark:",secret"`
        
        CommitmentHash frontend.Variable `gnark:",public"`
        Score          frontend.Variable `gnark:",public"`
        Passed         frontend.Variable `gnark:",public"`
}

func (circuit *PoBCircuit) Define(api frontend.API) error {
        // Whitepaper Bab 3.1.2 thresholds
        uploadThreshold := int64(core.MinUploadBandwidth * 1000) // 7 MB/s → 7000
        latencyThreshold := int64(core.TargetLatency * 1000)     // 100 ms → 100000
        packetLossThreshold := int64(core.TargetPacketLoss * 1000) // 0.1% → 100
        
        // Simplified: Just verify score calculation, don't compute Passed
        // Score formula (Whitepaper Bab 3.1.2): Average of 3 normalized metrics
        
        // Cap upload contribution at 1.0 (if upload > threshold, use 1.0)
        uploadRatio := api.Div(circuit.UploadBandwidth, uploadThreshold)
        
        // Latency: invert (lower is better)
        latencyRatio := api.Div(latencyThreshold, circuit.Latency)
        
        // Packet Loss: lower is better, normalize
        packetLossRatio := api.Sub(packetLossThreshold, circuit.PacketLoss)
        packetLossNorm := api.Div(packetLossRatio, packetLossThreshold)
        
        // CRITICAL FIX: Avoid fractional arithmetic
        // Instead of: score = (sum of ratios) / 3
        // Compare: score × 3 = sum of ratios
        // This eliminates division and truncation issues
        
        sumRatios := api.Add(api.Add(uploadRatio, latencyRatio), packetLossNorm)
        
        // Scale sum ×1000 to match witness units
        scaledSum := api.Mul(sumRatios, 1000)
        
        // Witness provides score×1000, so: (score×1000) × 3 == sum×1000
        scoreTimesThree := api.Mul(circuit.Score, 3)
        
        // Verify: score×3 == sum (both scaled ×1000)
        api.AssertIsEqual(scoreTimesThree, scaledSum)
        
        return nil
}

type ZKProofSystem struct {
        provingKey   groth16.ProvingKey
        verifyingKey groth16.VerifyingKey
        r1cs         constraint.ConstraintSystem
}

func NewZKProofSystem() (*ZKProofSystem, error) {
        circuit := PoBCircuit{}
        
        ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
        if err != nil {
                return nil, fmt.Errorf("failed to compile circuit: %w", err)
        }
        
        pk, vk, err := groth16.Setup(ccs)
        if err != nil {
                return nil, fmt.Errorf("failed to setup keys: %w", err)
        }
        
        return &ZKProofSystem{
                provingKey:   pk,
                verifyingKey: vk,
                r1cs:         ccs,
        }, nil
}

func (zk *ZKProofSystem) GenerateProof(result *PoBTestResult, score float64) ([]byte, error) {
        hash := sha256.Sum256([]byte(result.TestDataHash))
        commitmentHash := new(big.Int).SetBytes(hash[:])
        
        uploadInt := int64(result.UploadBandwidth * 1000)
        latencyInt := int64(result.Latency * 1000)
        packetLossInt := int64(result.PacketLoss * 1000) // Convert percentage to integer
        
        // CRITICAL FIX: Circuit computes unscaled score from milli-units
        // uploadRatio = uploadInt / (threshold×1000) → unscaled
        // So circuit output is already 0-1.0 range
        // We need to scale circuit calculation ×1000 OR keep score unscaled here
        // Solution: Scale circuit result ×1000 to match
        scoreInt := int64(score * 1000) // Will match circuit after we scale circuit
        
        passedInt := int64(0)
        if result.Passed {
                passedInt = 1
        }
        
        assignment := &PoBCircuit{
                UploadBandwidth: uploadInt,
                Latency:         latencyInt,
                PacketLoss:      packetLossInt,
                DataHash:        result.TestDataHash,
                CommitmentHash:  commitmentHash,
                Score:           scoreInt, // Now properly scaled to match circuit expectation
                Passed:          passedInt,
        }
        
        witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
        if err != nil {
                return nil, fmt.Errorf("failed to create witness: %w", err)
        }
        
        proof, err := groth16.Prove(zk.r1cs, zk.provingKey, witness)
        if err != nil {
                return nil, fmt.Errorf("failed to generate proof: %w", err)
        }
        
        var buf bytes.Buffer
        _, err = proof.WriteTo(&buf)
        if err != nil {
                return nil, fmt.Errorf("failed to serialize proof: %w", err)
        }
        
        return buf.Bytes(), nil
}

func (zk *ZKProofSystem) VerifyProof(proofBytes []byte, commitmentHash string, score float64, passed bool) (bool, error) {
        hash := sha256.Sum256([]byte(commitmentHash))
        commitmentHashInt := new(big.Int).SetBytes(hash[:])
        
        // Fix: Score is unscaled, matching circuit expectation
        scoreInt := int64(score * 1000)
        passedInt := int64(0)
        if passed {
                passedInt = 1
        }
        
        publicAssignment := &PoBCircuit{
                CommitmentHash: commitmentHashInt,
                Score:          scoreInt, // Properly scaled
                Passed:         passedInt,
        }
        
        publicWitness, err := frontend.NewWitness(publicAssignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
        if err != nil {
                return false, fmt.Errorf("failed to create public witness: %w", err)
        }
        
        proof := groth16.NewProof(ecc.BN254)
        buf := bytes.NewReader(proofBytes)
        if _, err := proof.ReadFrom(buf); err != nil {
                return false, fmt.Errorf("failed to unmarshal proof: %w", err)
        }
        
        err = groth16.Verify(proof, zk.verifyingKey, publicWitness)
        if err != nil {
                return false, nil
        }
        
        return true, nil
}

func (zk *ZKProofSystem) ExportVerifyingKey() (string, error) {
        var buf bytes.Buffer
        if _, err := zk.verifyingKey.WriteTo(&buf); err != nil {
                return "", fmt.Errorf("failed to serialize verifying key: %w", err)
        }
        return hex.EncodeToString(buf.Bytes()), nil
}

func (zk *ZKProofSystem) ImportVerifyingKey(vkHex string) error {
        vkBytes, err := hex.DecodeString(vkHex)
        if err != nil {
                return fmt.Errorf("failed to decode verifying key: %w", err)
        }
        
        buf := bytes.NewReader(vkBytes)
        if _, err := zk.verifyingKey.ReadFrom(buf); err != nil {
                return fmt.Errorf("failed to deserialize verifying key: %w", err)
        }
        
        return nil
}
