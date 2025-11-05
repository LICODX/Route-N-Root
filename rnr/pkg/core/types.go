package core

import (
        "crypto/ecdsa"
        "crypto/sha256"
        "encoding/json"
        "math/big"
        "time"
)

type BlockHeader struct {
        Version       int32
        PrevBlockHash []byte
        MerkleRoot    []byte
        Timestamp     time.Time
        Nonce         uint64
        Difficulty    *big.Int
        Height        uint64
        PoBScore      float64
        PoBWeight     uint64  // Whitepaper Bab 4.3: PoB weight for fork resolution (cumulative difficulty)
        VRFProof      []byte  // SECURITY: VRF proof for proposer selection verification
        VRFOutput     []byte  // VRF output (hash) used for randomness
}

type Block struct {
        Header       *BlockHeader
        Transactions []*Transaction
        ProposerID   string
        PoHSequence  []byte
        Signature    []byte
        VRFProof     []byte  // SECURITY: VRF proof for proposer selection (duplicated for easy access)
}

func (b *Block) Hash() ([]byte, error) {
        bCopy := *b
        bCopy.Signature = nil
        data, err := json.Marshal(bCopy)
        if err != nil {
                return nil, err
        }
        hash := sha256.Sum256(data)
        return hash[:], nil
}

type Transaction struct {
        ID        string
        From      string
        To        string
        Amount    *big.Int
        Timestamp time.Time
        Nonce     uint64
        Fee       *big.Int
        Signature []byte
        Data      []byte
}

func (tx *Transaction) Hash() ([]byte, error) {
        txCopy := *tx
        txCopy.Signature = nil
        data, err := json.Marshal(txCopy)
        if err != nil {
                return nil, err
        }
        hash := sha256.Sum256(data)
        return hash[:], nil
}

type ValidatorInfo struct {
        ID                string
        PublicKey         []byte // ECDSA public key for block signing
        VRFPublicKey      []byte // SECURITY: Ed25519 VRF public key for proposer selection verification
        PoBScore          float64
        UploadBandwidth   float64 // Upload bandwidth in MB/s (measured from PoB test) - Whitepaper: ≥ 7 MB/s
        Latency           float64 // Network latency in ms (measured from PoB test) - Whitepaper: ≤ 100 ms
        PacketLoss        float64 // Packet loss percentage (measured from PoB test) - Whitepaper: 0.1%
        Reputation        int
        LastPoBTest       time.Time
        IsActive          bool
        RewardAddress     string
        NetworkASN        string
        IPAddress         string
        IsSuspended       bool
        SuspensionEndTime time.Time
        SuspensionReason  string
        IsObserver        bool
        ObserverStartTime time.Time
        ObserverDuration  time.Duration
}

type Account struct {
        Address  string
        Balance  *big.Int
        Nonce    uint64
        CodeHash []byte
}

func EncodePublicKey(pubKey *ecdsa.PublicKey) ([]byte, error) {
        // Store both X and Y coordinates untuk proper ECDSA verification
        // Format: [X bytes][Y bytes]
        xBytes := pubKey.X.Bytes()
        yBytes := pubKey.Y.Bytes()
        
        // Pad to 32 bytes each untuk P-256 curve
        xPadded := make([]byte, 32)
        yPadded := make([]byte, 32)
        copy(xPadded[32-len(xBytes):], xBytes)
        copy(yPadded[32-len(yBytes):], yBytes)
        
        return append(xPadded, yPadded...), nil
}
