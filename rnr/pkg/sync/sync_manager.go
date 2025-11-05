package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"rnr-blockchain/pkg/blockchain"
	"rnr-blockchain/pkg/core"
)

type SyncStatus string

const (
	StatusSyncing SyncStatus = "syncing"
	StatusSynced  SyncStatus = "synced"
	StatusBehind  SyncStatus = "behind"
)

type SyncManager struct {
	blockchain     *blockchain.Blockchain
	status         SyncStatus
	targetHeight   uint64
	currentHeight  uint64
	syncPeer       string
	mu             sync.RWMutex
	requestTimeout time.Duration
}

type BlockRequest struct {
	StartHeight uint64 `json:"start_height"`
	EndHeight   uint64 `json:"end_height"`
}

type BlockResponse struct {
	Blocks []*core.Block `json:"blocks"`
}

type SyncStatusMessage struct {
	CurrentHeight uint64     `json:"current_height"`
	Status        SyncStatus `json:"status"`
}

func NewSyncManager(bc *blockchain.Blockchain) *SyncManager {
	return &SyncManager{
		blockchain:     bc,
		status:         StatusSynced,
		currentHeight:  bc.GetLatestBlock().Header.Height,
		requestTimeout: 30 * time.Second,
	}
}

func (sm *SyncManager) GetStatus() SyncStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.status
}

func (sm *SyncManager) GetCurrentHeight() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.currentHeight
}

func (sm *SyncManager) UpdateHeight(height uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.currentHeight = height
}

func (sm *SyncManager) StartSync(peerHeight uint64, peerID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	currentBlock := sm.blockchain.GetLatestBlock()
	sm.currentHeight = currentBlock.Header.Height

	if peerHeight <= sm.currentHeight {
		sm.status = StatusSynced
		return nil
	}

	sm.status = StatusSyncing
	sm.targetHeight = peerHeight
	sm.syncPeer = peerID

	log.Printf("🔄 Starting sync: current=%d, target=%d, peer=%s",
		sm.currentHeight, sm.targetHeight, peerID[:12])

	return nil
}

func (sm *SyncManager) CreateBlockRequest(batchSize uint64) *BlockRequest {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	startHeight := sm.currentHeight + 1
	endHeight := startHeight + batchSize - 1

	if endHeight > sm.targetHeight {
		endHeight = sm.targetHeight
	}

	return &BlockRequest{
		StartHeight: startHeight,
		EndHeight:   endHeight,
	}
}

func (sm *SyncManager) ProcessBlockResponse(blocks []*core.Block) error {
	if len(blocks) == 0 {
		return fmt.Errorf("empty block response")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, block := range blocks {
		if err := sm.blockchain.VerifyBlock(block); err != nil {
			return fmt.Errorf("invalid block %d: %w", block.Header.Height, err)
		}

		if err := sm.blockchain.AddBlock(block); err != nil {
			return fmt.Errorf("failed to add block %d: %w", block.Header.Height, err)
		}

		sm.currentHeight = block.Header.Height
		log.Printf("📦 Synced block #%d", block.Header.Height)
	}

	if sm.currentHeight >= sm.targetHeight {
		sm.status = StatusSynced
		log.Printf("✅ Sync completed! Height: %d", sm.currentHeight)
	}

	return nil
}

func (sm *SyncManager) HandleBlockRequest(req *BlockRequest) (*BlockResponse, error) {
	if req.StartHeight > req.EndHeight {
		return nil, fmt.Errorf("invalid range: start > end")
	}

	blocks := make([]*core.Block, 0)
	currentBlock := sm.blockchain.GetLatestBlock()

	if req.StartHeight > currentBlock.Header.Height {
		return &BlockResponse{Blocks: blocks}, nil
	}

	for height := req.StartHeight; height <= req.EndHeight; height++ {
		block, err := sm.blockchain.GetBlockByHeight(height)
		if err != nil {
			break
		}
		blocks = append(blocks, block)
	}

	return &BlockResponse{Blocks: blocks}, nil
}

func (sm *SyncManager) IsSyncing() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.status == StatusSyncing
}

func (sm *SyncManager) IsSynced() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.status == StatusSynced
}

func (sm *SyncManager) CheckIfBehind(peerHeight uint64) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if peerHeight > sm.currentHeight+10 {
		sm.status = StatusBehind
		return true
	}

	return false
}

func (req *BlockRequest) Marshal() ([]byte, error) {
	return json.Marshal(req)
}

func UnmarshalBlockRequest(data []byte) (*BlockRequest, error) {
	var req BlockRequest
	err := json.Unmarshal(data, &req)
	return &req, err
}

func (resp *BlockResponse) Marshal() ([]byte, error) {
	return json.Marshal(resp)
}

func UnmarshalBlockResponse(data []byte) (*BlockResponse, error) {
	var resp BlockResponse
	err := json.Unmarshal(data, &resp)
	return &resp, err
}
