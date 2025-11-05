package blockchain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"rnr-blockchain/pkg/core"
)

// StateTransaction provides atomic state updates using LevelDB batch writes
// This prevents state divergence if node crashes during updates
type StateTransaction struct {
	state         *State
	batch         *leveldb.Batch
	accountCache  map[string]*core.Account
	validatorCache map[string]*core.ValidatorInfo
	mu            sync.Mutex
}

// BeginTransaction starts an atomic state transaction
func (s *State) BeginTransaction() *StateTransaction {
	return &StateTransaction{
		state:          s,
		batch:          new(leveldb.Batch),
		accountCache:   make(map[string]*core.Account),
		validatorCache: make(map[string]*core.ValidatorInfo),
	}
}

// UpdateAccountAtomic stages an account update in the transaction
func (tx *StateTransaction) UpdateAccountAtomic(account *core.Account) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	accountBytes, err := json.Marshal(account)
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	// Stage write in batch
	tx.batch.Put([]byte("account_"+account.Address), accountBytes)
	// Cache for commit
	tx.accountCache[account.Address] = account

	return nil
}

// UpdateValidatorAtomic stages a validator update in the transaction
func (tx *StateTransaction) UpdateValidatorAtomic(validator *core.ValidatorInfo) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	validatorBytes, err := json.Marshal(validator)
	if err != nil {
		return fmt.Errorf("failed to marshal validator: %w", err)
	}

	// Stage write in batch
	tx.batch.Put([]byte("validator_"+validator.ID), validatorBytes)
	// Cache for commit
	tx.validatorCache[validator.ID] = validator

	return nil
}

// Commit atomically writes all staged changes to LevelDB and updates in-memory state
// Either ALL changes succeed or ALL fail (atomicity guarantee)
func (tx *StateTransaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	// CRITICAL: Write to LevelDB first (durable storage)
	if err := tx.state.db.Write(tx.batch, nil); err != nil {
		return fmt.Errorf("failed to commit transaction to LevelDB: %w", err)
	}

	// SUCCESS: Now update in-memory state (already persisted to disk)
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()

	// Update accounts
	for addr, account := range tx.accountCache {
		tx.state.accounts[addr] = account
	}

	// Update validators
	for id, validator := range tx.validatorCache {
		tx.state.validators[id] = validator
	}

	return nil
}

// Rollback discards all staged changes without writing to disk
func (tx *StateTransaction) Rollback() {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	// Clear batch and caches
	tx.batch.Reset()
	tx.accountCache = make(map[string]*core.Account)
	tx.validatorCache = make(map[string]*core.ValidatorInfo)
}

// ApplyTransactionAtomic applies a transaction atomically with proper error handling
// This replaces the old ApplyTransaction to ensure atomicity
func (s *State) ApplyTransactionAtomic(tx *core.Transaction) error {
	// Begin atomic transaction
	stateTx := s.BeginTransaction()

	// Get sender account
	sender, err := s.GetAccount(tx.From)
	if err != nil {
		return fmt.Errorf("failed to get sender account: %w", err)
	}

	// Validate transaction
	if sender.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient balance: has %s, needs %s", sender.Balance.String(), tx.Amount.String())
	}

	if sender.Nonce != tx.Nonce {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce, tx.Nonce)
	}

	// Get recipient account
	recipient, err := s.GetAccount(tx.To)
	if err != nil {
		return fmt.Errorf("failed to get recipient account: %w", err)
	}

	// Calculate new balances
	sender.Balance = new(big.Int).Sub(sender.Balance, tx.Amount)
	sender.Nonce++
	recipient.Balance = new(big.Int).Add(recipient.Balance, tx.Amount)

	// Stage updates in transaction
	if err := stateTx.UpdateAccountAtomic(sender); err != nil {
		stateTx.Rollback()
		return err
	}

	if err := stateTx.UpdateAccountAtomic(recipient); err != nil {
		stateTx.Rollback()
		return err
	}

	// Commit atomically (all or nothing)
	if err := stateTx.Commit(); err != nil {
		stateTx.Rollback()
		return fmt.Errorf("transaction commit failed: %w", err)
	}

	return nil
}

// BatchUpdateAccountsAtomic updates multiple accounts atomically
// Useful for block processing where many accounts are updated at once
func (s *State) BatchUpdateAccountsAtomic(accounts []*core.Account) error {
	tx := s.BeginTransaction()

	for _, account := range accounts {
		if err := tx.UpdateAccountAtomic(account); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// BatchUpdateValidatorsAtomic updates multiple validators atomically
func (s *State) BatchUpdateValidatorsAtomic(validators []*core.ValidatorInfo) error {
	tx := s.BeginTransaction()

	for _, validator := range validators {
		if err := tx.UpdateValidatorAtomic(validator); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}
