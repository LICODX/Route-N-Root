package genesis

import (
        "crypto/ecdsa"
        "encoding/json"
        "fmt"
        "math/big"
        "os"
        "time"

        "rnr-blockchain/pkg/core"
)

type GenesisValidator struct {
        Address   string `json:"address"`
        PublicKey string `json:"public_key"`
        Stake     string `json:"stake"`
}

type GenesisConfig struct {
        ChainID          string             `json:"chain_id"`
        NetworkName      string             `json:"network_name"`
        GenesisTimestamp int64              `json:"genesis_timestamp"`
        GenesisWallet    string             `json:"genesis_wallet"`
        InitialValidators []GenesisValidator `json:"initial_validators"`
        BootstrapNodes   []string           `json:"bootstrap_nodes"`
        BlockTime        int                `json:"block_time_seconds"`
        InitialDifficulty int64             `json:"initial_difficulty"`
}

func DefaultGenesisConfig() *GenesisConfig {
        return &GenesisConfig{
                ChainID:          "rnr-mainnet-1",
                NetworkName:      "RNR Mainnet",
                GenesisTimestamp: 1231006505, // Easter Egg: Bitcoin Genesis Block timestamp (Jan 3, 2009 18:15:05 UTC)
                BlockTime:        30,
                InitialDifficulty: 1,
                InitialValidators: []GenesisValidator{},
                BootstrapNodes:   []string{},
        }
}

func LoadGenesisConfig(path string) (*GenesisConfig, error) {
        data, err := os.ReadFile(path)
        if err != nil {
                return nil, fmt.Errorf("failed to read genesis config: %w", err)
        }

        var config GenesisConfig
        if err := json.Unmarshal(data, &config); err != nil {
                return nil, fmt.Errorf("failed to parse genesis config: %w", err)
        }

        return &config, nil
}

func (gc *GenesisConfig) Save(path string) error {
        data, err := json.MarshalIndent(gc, "", "  ")
        if err != nil {
                return fmt.Errorf("failed to marshal genesis config: %w", err)
        }

        if err := os.WriteFile(path, data, 0644); err != nil {
                return fmt.Errorf("failed to write genesis config: %w", err)
        }

        return nil
}

func (gc *GenesisConfig) CreateGenesisBlock() *core.Block {
        header := &core.BlockHeader{
                Version:       1,
                PrevBlockHash: []byte{},
                MerkleRoot:    []byte{},
                Timestamp:     time.Unix(gc.GenesisTimestamp, 0),
                Nonce:         0,
                Difficulty:    big.NewInt(gc.InitialDifficulty),
                Height:        0,
                PoBScore:      1.0,
        }
        
        return &core.Block{
                Header:       header,
                Transactions: []*core.Transaction{},
                ProposerID:   "genesis",
                PoHSequence:  []byte{},
                Signature:    []byte{},
        }
}

func (gc *GenesisConfig) AddValidator(address string, pubKey *ecdsa.PublicKey, stake *big.Int) error {
        pubKeyBytes, err := core.EncodePublicKey(pubKey)
        if err != nil {
                return fmt.Errorf("failed to encode public key: %w", err)
        }

        validator := GenesisValidator{
                Address:   address,
                PublicKey: fmt.Sprintf("%x", pubKeyBytes),
                Stake:     stake.String(),
        }

        gc.InitialValidators = append(gc.InitialValidators, validator)
        return nil
}

func (gc *GenesisConfig) Validate() error {
        if gc.ChainID == "" {
                return fmt.Errorf("chain_id is required")
        }
        
        if gc.NetworkName == "" {
                return fmt.Errorf("network_name is required")
        }
        
        if gc.GenesisTimestamp == 0 {
                return fmt.Errorf("genesis_timestamp is required")
        }
        
        if len(gc.InitialValidators) == 0 {
                return fmt.Errorf("at least one validator is required")
        }
        
        return nil
}
