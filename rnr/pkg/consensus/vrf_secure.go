package consensus

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/yahoo/coname/vrf"
)

// SECURITY: Proper VRF implementation using Yahoo's edwards25519 VRF
// This implementation ensures verifiers can independently verify VRF output correctness

type SecureVRFSystem struct {
	privateKey *[vrf.SecretKeySize]byte
	publicKey  []byte
}

type VRFOutput struct {
	Value []byte // Verifiable VRF output
	Proof []byte // Cryptographic proof
}

func NewSecureVRFSystem() (*SecureVRFSystem, error) {
	// Generate proper VRF keypair using Yahoo's implementation
	pubKey, privKey, err := vrf.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate VRF keys: %w", err)
	}
	return &SecureVRFSystem{
		privateKey: privKey,
		publicKey:  pubKey,
	}, nil
}

// Generate creates VRF output and proof
// SECURITY: Uses proper VRF that allows verifiers to recompute and verify output
func (v *SecureVRFSystem) Generate(input []byte) (*VRFOutput, error) {
	// Yahoo VRF Prove returns (vrfOutput, proof)
	// Verifiers can independently verify vrfOutput is correct for this input
	vrfOutput, proof := vrf.Prove(input, v.privateKey)
	
	return &VRFOutput{
		Value: vrfOutput,
		Proof: proof,
	}, nil
}

// Verify checks VRF proof and independently verifies output correctness
// SECURITY: This prevents proposers from choosing arbitrary VRF outputs
func (v *SecureVRFSystem) Verify(input []byte, output *VRFOutput, pubKey []byte) bool {
	// Yahoo VRF Verify checks:
	// 1. Proof is valid
	// 2. VRF output matches what should be generated for this input
	return vrf.Verify(pubKey, input, output.Value, output.Proof)
}

// VerifyWithPublicKey verifies VRF using external public key
func VerifyVRFWithPublicKey(input []byte, vrfOutput []byte, proof []byte, pubKey []byte) bool {
	return vrf.Verify(pubKey, input, vrfOutput, proof)
}

func DeterministicSelectProposer(seed []byte, validators []string, validatorKeys map[string][]byte) (string, error) {
	if len(validators) == 0 {
		return "", fmt.Errorf("no validators available")
	}

	lowestHash := new(big.Int).SetBytes(make([]byte, 32))
	for i := range lowestHash.Bytes() {
		lowestHash.Bytes()[i] = 0xFF
	}
	
	selectedProposer := validators[0]

	for _, validatorID := range validators {
		input := append(seed, []byte(validatorID)...)
		
		validatorPubKey := validatorKeys[validatorID]
		if len(validatorPubKey) > 0 {
			input = append(input, validatorPubKey...)
		}
		
		hash := sha256.Sum256(input)
		hashValue := new(big.Int).SetBytes(hash[:])
		
		if hashValue.Cmp(lowestHash) < 0 {
			lowestHash = hashValue
			selectedProposer = validatorID
		}
	}

	return selectedProposer, nil
}

func PoBWeightedSelectProposer(seed []byte, validators []string, validatorKeys map[string][]byte, pobScores map[string]float64) (string, error) {
	if len(validators) == 0 {
		return "", fmt.Errorf("no validators available")
	}

	lowestWeightedHash := new(big.Int).SetBytes(make([]byte, 32))
	for i := range lowestWeightedHash.Bytes() {
		lowestWeightedHash.Bytes()[i] = 0xFF
	}
	
	selectedProposer := validators[0]
	maxPoBScore := 0.0

	for _, validatorID := range validators {
		pobScore := pobScores[validatorID]
		if pobScore == 0 {
			pobScore = 0.5
		}

		if pobScore > maxPoBScore {
			maxPoBScore = pobScore
		}

		input := append(seed, []byte(validatorID)...)
		
		validatorPubKey := validatorKeys[validatorID]
		if len(validatorPubKey) > 0 {
			input = append(input, validatorPubKey...)
		}
		
		hash := sha256.Sum256(input)
		hashValue := new(big.Int).SetBytes(hash[:])

		weightFactor := big.NewFloat(2.0 - pobScore)
		if pobScore < 0.1 {
			weightFactor = big.NewFloat(10.0)
		}

		hashFloat := new(big.Float).SetInt(hashValue)
		weightedHashFloat := new(big.Float).Mul(hashFloat, weightFactor)
		
		weightedHash, _ := weightedHashFloat.Int(nil)
		
		if weightedHash.Cmp(lowestWeightedHash) < 0 {
			lowestWeightedHash = weightedHash
			selectedProposer = validatorID
		}
	}

	return selectedProposer, nil
}

func SecureSelectProposer(blockHash []byte, validators []string, validatorKeys map[string][]byte, vrfSys *SecureVRFSystem) (string, error) {
	return DeterministicSelectProposer(blockHash, validators, validatorKeys)
}

func SecureSelectTestCommittee(candidateID string, blockHash []byte, validators []string, count int, vrfSys *SecureVRFSystem) ([]string, error) {
	if count > len(validators) {
		count = len(validators)
	}

	type validatorScore struct {
		id    string
		score *big.Int
	}

	scores := make([]validatorScore, 0, len(validators))

	for _, validatorID := range validators {
		if validatorID == candidateID {
			continue
		}

		input := append(blockHash, []byte(candidateID+validatorID)...)
		hash := sha256.Sum256(input)
		score := new(big.Int).SetBytes(hash[:])
		scores = append(scores, validatorScore{id: validatorID, score: score})
	}

	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].score.Cmp(scores[j].score) > 0 {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	selected := make([]string, 0, count)
	for i := 0; i < count && i < len(scores); i++ {
		selected = append(selected, scores[i].id)
	}

	return selected, nil
}

func (v *SecureVRFSystem) GetPublicKey() []byte {
	return v.publicKey
}

func GenerateProposerSeed(blockHeight uint64, previousBlockHash []byte) []byte {
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, blockHeight)
	return append(previousBlockHash, heightBytes...)
}
