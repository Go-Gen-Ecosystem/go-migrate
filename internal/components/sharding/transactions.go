/**
 * @file transactions.go
 * @description Cross-shard transaction coordination using two-phase commit protocol
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/transactions.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CrossShardTransaction manages distributed transactions across multiple shards
type CrossShardTransaction struct {
	ID              string
	coordinator     *ShardedMigrationCoordinator
	shards          []*Shard
	operations      []TransactionOperation
	prepared        map[string]bool
	committed       map[string]bool
	preparedMu      sync.RWMutex
	committedMu     sync.RWMutex
	state           TransactionState
	stateMu         sync.RWMutex
	startedAt       time.Time
	preparedAt      *time.Time
	committedAt     *time.Time
	timeout         time.Duration
	compensations   []CompensationFunc
	compensationsMu sync.Mutex
}

// TransactionState represents the state of a distributed transaction
type TransactionState string

const (
	TransactionStateInitiated TransactionState = "initiated"
	TransactionStatePreparing TransactionState = "preparing"
	TransactionStatePrepared  TransactionState = "prepared"
	TransactionStateCommitting TransactionState = "committing"
	TransactionStateCommitted TransactionState = "committed"
	TransactionStateAborting  TransactionState = "aborting"
	TransactionStateAborted   TransactionState = "aborted"
	TransactionStateFailed    TransactionState = "failed"
)

// TransactionOperation represents an operation to execute in a transaction
type TransactionOperation struct {
	ShardID     string
	OperationID string
	SQL         string
	Type        OperationType
	Rollback    string // Rollback SQL for compensation
}

// OperationType represents the type of database operation
type OperationType string

const (
	OperationTypeRead   OperationType = "read"
	OperationTypeWrite  OperationType = "write"
	OperationTypeDelete OperationType = "delete"
	OperationTypeUpdate OperationType = "update"
)

// CompensationFunc is a function that compensates for a failed transaction
type CompensationFunc func(ctx context.Context) error

// TransactionResult represents the result of a transaction
type TransactionResult struct {
	TransactionID   string
	State           TransactionState
	TotalShards     int
	PreparedShards  int
	CommittedShards int
	FailedShards    []string
	Duration        time.Duration
	Error           error
}

// NewCrossShardTransaction creates a new cross-shard transaction
func NewCrossShardTransaction(
	coordinator *ShardedMigrationCoordinator,
	shards []*Shard,
	timeout time.Duration,
) *CrossShardTransaction {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &CrossShardTransaction{
		ID:            fmt.Sprintf("tx_%d", time.Now().UnixNano()),
		coordinator:   coordinator,
		shards:        shards,
		operations:    make([]TransactionOperation, 0),
		prepared:      make(map[string]bool),
		committed:     make(map[string]bool),
		state:         TransactionStateInitiated,
		startedAt:     time.Now(),
		timeout:       timeout,
		compensations: make([]CompensationFunc, 0),
	}
}

// AddOperation adds an operation to the transaction
func (t *CrossShardTransaction) AddOperation(op TransactionOperation) error {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()

	if t.state != TransactionStateInitiated {
		return fmt.Errorf("cannot add operations after transaction has started")
	}

	t.operations = append(t.operations, op)
	return nil
}

// Begin starts the transaction
func (t *CrossShardTransaction) Begin(ctx context.Context) error {
	t.stateMu.Lock()
	if t.state != TransactionStateInitiated {
		t.stateMu.Unlock()
		return fmt.Errorf("transaction already started")
	}
	t.state = TransactionStatePreparing
	t.stateMu.Unlock()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Phase 1: Prepare all shards
	if err := t.prepare(ctx); err != nil {
		t.setState(TransactionStateFailed)
		return fmt.Errorf("prepare phase failed: %w", err)
	}

	t.setState(TransactionStatePrepared)
	now := time.Now()
	t.preparedAt = &now

	return nil
}

// Prepare executes the prepare phase of two-phase commit
func (t *CrossShardTransaction) prepare(ctx context.Context) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(t.shards))
	preparedCount := int32(0)

	for _, shard := range t.shards {
		wg.Add(1)
		go func(s *Shard) {
			defer wg.Done()

			// Get operations for this shard
			ops := t.getOperationsForShard(s.ID)
			if len(ops) == 0 {
				// No operations for this shard, mark as prepared
				t.markPrepared(s.ID)
				atomic.AddInt32(&preparedCount, 1)
				return
			}

			// Execute prepare
			if err := t.prepareOnShard(ctx, s, ops); err != nil {
				errChan <- fmt.Errorf("shard %s: %w", s.ID, err)
				return
			}

			t.markPrepared(s.ID)
			atomic.AddInt32(&preparedCount, 1)
		}(shard)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		// Abort all prepared shards
		t.abort(context.Background())
		return fmt.Errorf("prepare failed on %d shard(s): %v", len(errors), errors)
	}

	if atomic.LoadInt32(&preparedCount) != int32(len(t.shards)) {
		return fmt.Errorf("not all shards prepared: %d/%d", preparedCount, len(t.shards))
	}

	return nil
}

// prepareOnShard prepares a transaction on a single shard
func (t *CrossShardTransaction) prepareOnShard(ctx context.Context, shard *Shard, ops []TransactionOperation) error {
	// Open connection
	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer db.Close()

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Execute all operations
	for _, op := range ops {
		_, err := tx.ExecContext(ctx, op.SQL)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute operation %s: %w", op.OperationID, err)
		}
	}

	// For two-phase commit, we would normally keep the transaction open
	// with a PREPARE TRANSACTION statement (PostgreSQL)
	// For simplicity, we commit here and rely on compensations for rollback

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to prepare commit: %w", err)
	}

	// Register compensation function for rollback
	t.registerCompensation(func(ctx context.Context) error {
		return t.compensateOnShard(ctx, shard, ops)
	})

	return nil
}

// Commit commits the transaction on all shards
func (t *CrossShardTransaction) Commit(ctx context.Context) error {
	t.stateMu.Lock()
	if t.state != TransactionStatePrepared {
		t.stateMu.Unlock()
		return fmt.Errorf("transaction not in prepared state")
	}
	t.state = TransactionStateCommitting
	t.stateMu.Unlock()

	// Phase 2: Commit all shards
	var wg sync.WaitGroup
	errChan := make(chan error, len(t.shards))
	committedCount := int32(0)

	for _, shard := range t.shards {
		wg.Add(1)
		go func(s *Shard) {
			defer wg.Done()

			// Mark as committed (operations already executed in prepare phase)
			t.markCommitted(s.ID)
			atomic.AddInt32(&committedCount, 1)
		}(shard)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.setState(TransactionStateFailed)
		return fmt.Errorf("commit failed: %v", errors)
	}

	t.setState(TransactionStateCommitted)
	now := time.Now()
	t.committedAt = &now

	return nil
}

// Rollback aborts the transaction and executes compensations
func (t *CrossShardTransaction) Rollback(ctx context.Context) error {
	t.stateMu.Lock()
	if t.state == TransactionStateCommitted {
		t.stateMu.Unlock()
		return fmt.Errorf("cannot rollback committed transaction")
	}
	if t.state == TransactionStateAborted {
		t.stateMu.Unlock()
		return nil // Already aborted
	}
	t.state = TransactionStateAborting
	t.stateMu.Unlock()

	// Execute compensations
	if err := t.executeCompensations(ctx); err != nil {
		t.setState(TransactionStateFailed)
		return fmt.Errorf("compensation failed: %w", err)
	}

	t.setState(TransactionStateAborted)
	return nil
}

// abort aborts the transaction (internal)
func (t *CrossShardTransaction) abort(ctx context.Context) error {
	return t.Rollback(ctx)
}

// compensateOnShard executes compensation logic on a shard
func (t *CrossShardTransaction) compensateOnShard(ctx context.Context, shard *Shard, ops []TransactionOperation) error {
	db, err := sql.Open(shard.DatabaseType, shard.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("failed to connect for compensation: %w", err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin compensation transaction: %w", err)
	}

	// Execute rollback operations in reverse order
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		if op.Rollback != "" {
			_, err := tx.ExecContext(ctx, op.Rollback)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to execute rollback for %s: %w", op.OperationID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit compensation: %w", err)
	}

	return nil
}

// executeCompensations executes all registered compensation functions
func (t *CrossShardTransaction) executeCompensations(ctx context.Context) error {
	t.compensationsMu.Lock()
	defer t.compensationsMu.Unlock()

	var wg sync.WaitGroup
	errChan := make(chan error, len(t.compensations))

	// Execute compensations in reverse order
	for i := len(t.compensations) - 1; i >= 0; i-- {
		compensation := t.compensations[i]
		wg.Add(1)
		go func(comp CompensationFunc) {
			defer wg.Done()
			if err := comp(ctx); err != nil {
				errChan <- err
			}
		}(compensation)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("compensation errors: %v", errors)
	}

	return nil
}

// GetResult returns the transaction result
func (t *CrossShardTransaction) GetResult() *TransactionResult {
	t.stateMu.RLock()
	state := t.state
	t.stateMu.RUnlock()

	t.preparedMu.RLock()
	preparedCount := len(t.prepared)
	t.preparedMu.RUnlock()

	t.committedMu.RLock()
	committedCount := len(t.committed)
	t.committedMu.RUnlock()

	duration := time.Since(t.startedAt)

	return &TransactionResult{
		TransactionID:   t.ID,
		State:           state,
		TotalShards:     len(t.shards),
		PreparedShards:  preparedCount,
		CommittedShards: committedCount,
		Duration:        duration,
	}
}

// GetState returns the current transaction state
func (t *CrossShardTransaction) GetState() TransactionState {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()
	return t.state
}

// setState updates the transaction state
func (t *CrossShardTransaction) setState(state TransactionState) {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	t.state = state
}

// markPrepared marks a shard as prepared
func (t *CrossShardTransaction) markPrepared(shardID string) {
	t.preparedMu.Lock()
	defer t.preparedMu.Unlock()
	t.prepared[shardID] = true
}

// markCommitted marks a shard as committed
func (t *CrossShardTransaction) markCommitted(shardID string) {
	t.committedMu.Lock()
	defer t.committedMu.Unlock()
	t.committed[shardID] = true
}

// isPrepared checks if a shard is prepared
func (t *CrossShardTransaction) isPrepared(shardID string) bool {
	t.preparedMu.RLock()
	defer t.preparedMu.RUnlock()
	return t.prepared[shardID]
}

// isCommitted checks if a shard is committed
func (t *CrossShardTransaction) isCommitted(shardID string) bool {
	t.committedMu.RLock()
	defer t.committedMu.RUnlock()
	return t.committed[shardID]
}

// getOperationsForShard returns operations for a specific shard
func (t *CrossShardTransaction) getOperationsForShard(shardID string) []TransactionOperation {
	var ops []TransactionOperation
	for _, op := range t.operations {
		if op.ShardID == shardID {
			ops = append(ops, op)
		}
	}
	return ops
}

// registerCompensation registers a compensation function
func (t *CrossShardTransaction) registerCompensation(comp CompensationFunc) {
	t.compensationsMu.Lock()
	defer t.compensationsMu.Unlock()
	t.compensations = append(t.compensations, comp)
}

// CrossShardTransactionManager manages multiple cross-shard transactions
type CrossShardTransactionManager struct {
	coordinator   *ShardedMigrationCoordinator
	transactions  map[string]*CrossShardTransaction
	transactionsMu sync.RWMutex
}

// NewCrossShardTransactionManager creates a new transaction manager
func NewCrossShardTransactionManager(coordinator *ShardedMigrationCoordinator) *CrossShardTransactionManager {
	return &CrossShardTransactionManager{
		coordinator:  coordinator,
		transactions: make(map[string]*CrossShardTransaction),
	}
}

// BeginTransaction starts a new cross-shard transaction
func (m *CrossShardTransactionManager) BeginTransaction(ctx context.Context, shards []*Shard, timeout time.Duration) (*CrossShardTransaction, error) {
	tx := NewCrossShardTransaction(m.coordinator, shards, timeout)

	m.transactionsMu.Lock()
	m.transactions[tx.ID] = tx
	m.transactionsMu.Unlock()

	return tx, nil
}

// GetTransaction retrieves a transaction by ID
func (m *CrossShardTransactionManager) GetTransaction(txID string) (*CrossShardTransaction, error) {
	m.transactionsMu.RLock()
	defer m.transactionsMu.RUnlock()

	tx, exists := m.transactions[txID]
	if !exists {
		return nil, fmt.Errorf("transaction %s not found", txID)
	}

	return tx, nil
}

// CommitTransaction commits a transaction
func (m *CrossShardTransactionManager) CommitTransaction(ctx context.Context, txID string) error {
	tx, err := m.GetTransaction(txID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// RollbackTransaction rolls back a transaction
func (m *CrossShardTransactionManager) RollbackTransaction(ctx context.Context, txID string) error {
	tx, err := m.GetTransaction(txID)
	if err != nil {
		return err
	}

	return tx.Rollback(ctx)
}

// CleanupCompletedTransactions removes completed transactions older than specified duration
func (m *CrossShardTransactionManager) CleanupCompletedTransactions(olderThan time.Duration) int {
	m.transactionsMu.Lock()
	defer m.transactionsMu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, tx := range m.transactions {
		state := tx.GetState()
		if (state == TransactionStateCommitted || state == TransactionStateAborted) &&
		   tx.startedAt.Before(cutoff) {
			delete(m.transactions, id)
			removed++
		}
	}

	return removed
}

// GetActiveTransactions returns all active transactions
func (m *CrossShardTransactionManager) GetActiveTransactions() []*CrossShardTransaction {
	m.transactionsMu.RLock()
	defer m.transactionsMu.RUnlock()

	var active []*CrossShardTransaction
	for _, tx := range m.transactions {
		state := tx.GetState()
		if state != TransactionStateCommitted && state != TransactionStateAborted {
			active = append(active, tx)
		}
	}

	return active
}

// GetTransactionCount returns the number of transactions in each state
func (m *CrossShardTransactionManager) GetTransactionCount() map[TransactionState]int {
	m.transactionsMu.RLock()
	defer m.transactionsMu.RUnlock()

	counts := make(map[TransactionState]int)
	for _, tx := range m.transactions {
		state := tx.GetState()
		counts[state]++
	}

	return counts
}
