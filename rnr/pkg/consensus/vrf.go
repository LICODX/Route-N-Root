package consensus

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

// WARNING: This is a SIMPLIFIED VRF implementation for demonstration purposes
// Production systems should use a proper VRF library like github.com/cloudflare/circl/vrf

type VRFSystem struct {
	seed []byte
}

func NewVRFSystem() (*VRFSystem, error) {
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	if err != nil {
		return nil, err
	}
	return &VRFSystem{seed: seed}, nil
}

func (vrf *VRFSystem) GenerateVRF(input []byte) (*big.Int, error) {
	combined := append(vrf.seed, input...)
	hash := sha256.Sum256(combined)
	return new(big.Int).SetBytes(hash[:]), nil
}

func SelectProposerVRF(blockHash []byte, validators []string, vrfSys *VRFSystem) (string, error) {
	if len(validators) == 0 {
		return "", nil
	}

	lowestValue := new(big.Int).SetBytes(make([]byte, 32))
	for i := range lowestValue.Bytes() {
		lowestValue.Bytes()[i] = 0xFF
	}
	
	selectedProposer := validators[0]

	for _, validatorID := range validators {
		input := append(blockHash, []byte(validatorID)...)
		value, err := vrfSys.GenerateVRF(input)
		if err != nil {
			continue
		}

		if value.Cmp(lowestValue) < 0 {
			lowestValue = value
			selectedProposer = validatorID
		}
	}

	return selectedProposer, nil
}

func SelectTestCommittee(candidateID string, blockHash []byte, validators []string, count int) []string {
	if count > len(validators) {
		count = len(validators)
	}

	seed := append(blockHash, []byte(candidateID)...)
	hash := sha256.Sum256(seed)
	
	selected := make([]string, 0, count)
	usedIndices := make(map[int]bool)

	for len(selected) < count && len(selected) < len(validators) {
		index := new(big.Int).SetBytes(hash[:]).Mod(new(big.Int).SetBytes(hash[:]), big.NewInt(int64(len(validators)))).Int64()
		
		if !usedIndices[int(index)] && validators[index] != candidateID {
			selected = append(selected, validators[index])
			usedIndices[int(index)] = true
		}
		
		hash = sha256.Sum256(hash[:])
	}

	return selected
}

func GenerateVRFProof(input []byte) string {
	hash := sha256.Sum256(input)
	return hex.EncodeToString(hash[:])
}
