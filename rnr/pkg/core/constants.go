package core

import (
        "math/big"
        "time"
)

const (
        BlockTime                 = 30 * time.Second
        PropagationPhase          = 10 * time.Second
        VerificationVotingPhase   = 15 * time.Second
        BufferFinalityPhase       = 5 * time.Second
        MinUploadBandwidth        = 7.0  // Whitepaper Bab 3.1.2: ≥ 7 MB/s
        TargetLatency             = 100.0 // Whitepaper Bab 3.1.2: ≤ 100 ms
        TargetPacketLoss          = 0.1   // Whitepaper Bab 3.1.2: 0.1% packet loss
        MinPoBScore               = 0.85
        TestDataSize              = 8 * 1024 * 1024
        MaxTestCommitteeSize      = 8
        MinTestCommitteeSize      = 5
        ReverificationInterval    = 100
        DynamicBlockCapacityRatio = 0.30
        MinPeerMeasurement        = 8
        PeerSamplingCount         = 10
        MinBlockSize              = 50
        MaxBlockSize              = 1000
        BaseBlockSize             = 100
        MaxNewAddressesPerBlock   = 15
)

var (
        InitialBlockReward       = big.NewInt(100)
        BlockRewardReduction     = big.NewInt(1)
        BlockReductionInterval   = big.NewInt(1000000)
        MinBlockReward           = big.NewInt(1)
        ProposerRewardPercentage = big.NewInt(80)
        PoBContributorPercentage = big.NewInt(20)
        MaxPoBContributors       = 20
        BaseFeePercentage        = big.NewInt(1)
)

const (
        SupermajorityThreshold = 0.85
)

const (
        MnemonicLength   = 12
        DerivationPath   = "m/44'/60'/0'/0/0"
        AddressHexLength = 40
)

const (
        MessageTypeSyncRequest = iota
        MessageTypeSyncResponse
        MessageTypeBlock
        MessageTypeTransaction
        MessageTypeTxRequest        // Request specific transaction by ID
        MessageTypeTxResponse       // Response with transaction data
        MessageTypePoBTestRequest
        MessageTypePoBTestResponse
        MessageTypeVote
        MessageTypeGovernanceProposal
        MessageTypeGovernanceVote
        MessageTypePing
        MessageTypePong
)
