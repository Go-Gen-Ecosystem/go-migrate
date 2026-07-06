/**
 * @file executor.go
 * @description Migration executor implementations for different database types
 * @author Admilson B. F. Cossa
 * @date 2024-01-11
 * @path pkg/core/database/migrations/executor.go
 */

package executor

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
	"gorm.io/gorm"
)

// Migration is an alias for types.Migration, kept for package-local compatibility.
type Migration = types.Migration

// MigrationConfig is an alias for types.MigrationConfig.
type MigrationConfig = types.MigrationConfig

// MigrationExecutor is an alias for types.MigrationExecutor.
type MigrationExecutor = types.MigrationExecutor

// ExecutionPlan is an alias for types.ExecutionPlan.
type ExecutionPlan = types.ExecutionPlan

// Migration states
const (
	MigrationStateFailed     = "failed"
	MigrationStateRolledBack = "rolled_back"
	EnvironmentProduction    = "production"
)

// DatabaseMigrationExecutor implements MigrationExecutor for SQL databases
type DatabaseMigrationExecutor struct {
	db     *gorm.DB
	config *MigrationConfig
	logger logging.ComponentLogger
}

// TransactionMigrationExecutor wraps migrations in transactions
type TransactionMigrationExecutor struct {
	executor MigrationExecutor
	db       *gorm.DB
}

// DryRunMigrationExecutor simulates migration execution without applying changes
type DryRunMigrationExecutor struct {
	executor MigrationExecutor
	output   []string
	analyzer *DryRunAnalyzer
}

// NewDatabaseMigrationExecutor creates a new database migration executor
func NewDatabaseMigrationExecutor(db *gorm.DB, config *MigrationConfig) *DatabaseMigrationExecutor {
	return &DatabaseMigrationExecutor{
		db:     db,
		config: config,
		logger: logging.GetComponentLogger("migration-executor"),
	}
}

// NewTransactionMigrationExecutor creates a new transaction migration executor
func NewTransactionMigrationExecutor(executor MigrationExecutor, db *gorm.DB) *TransactionMigrationExecutor {
	return &TransactionMigrationExecutor{
		executor: executor,
		db:       db,
	}
}

// NewDryRunMigrationExecutor creates a new dry run migration executor
func NewDryRunMigrationExecutor(executor MigrationExecutor) *DryRunMigrationExecutor {
	// Try to detect database dialect from the underlying executor
	dbDialect := "unknown"
	if dbExec, ok := executor.(*DatabaseMigrationExecutor); ok && dbExec.db != nil {
		dbDialect = dbExec.db.Name()
	}

	return &DryRunMigrationExecutor{
		executor: executor,
		output:   make([]string, 0),
		analyzer: NewDryRunAnalyzer(dbDialect),
	}
}

// Execute executes a single migration (implements MigrationExecutor interface)
func (e *DatabaseMigrationExecutor) Execute(ctx context.Context, migration *Migration) error {
	startTime := time.Now()

	// Log migration start
	e.logMigrationStart(ctx, migration)

	// Execute the migration content
	if err := e.executeSQL(ctx, migration.Script); err != nil {
		migration.State = MigrationStateFailed
		migration.ErrorMessage = err.Error()
		migration.ExecutedAt = &startTime
		migration.UpdatedAt = time.Now()
		return fmt.Errorf("failed to execute migration %s: %w", migration.ID, err)
	}

	// Update migration state
	executedAt := time.Now()
	migration.State = "applied"
	migration.ExecutedAt = &executedAt
	migration.ExecutionTime = time.Since(startTime)
	migration.UpdatedAt = time.Now()

	// Log migration completion
	e.logMigrationComplete(ctx, migration)

	return nil
}

// ExecuteDryRun simulates migration execution without applying changes (implements MigrationExecutor interface)
func (e *DatabaseMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
		Migrations:    []*Migration{migration},
		TotalCount:    1,
		EstimatedTime: 0,
		Warnings:      []string{},
		Errors:        []string{},
	}
	return plan, nil
}

// Rollback rolls back a migration (implements MigrationExecutor interface)
func (e *DatabaseMigrationExecutor) Rollback(ctx context.Context, migration *Migration) error {
	if !migration.CanRollback {
		return fmt.Errorf("migration %s cannot be rolled back", migration.ID)
	}

	if migration.RollbackSQL == "" {
		return fmt.Errorf("no rollback SQL provided for migration %s", migration.ID)
	}

	startTime := time.Now()

	// Log rollback start
	e.logRollbackStart(ctx, migration)

	// Execute rollback SQL
	if err := e.executeSQL(ctx, migration.RollbackSQL); err != nil {
		return fmt.Errorf("failed to rollback migration %s: %w", migration.ID, err)
	}

	// Update migration state
	migration.State = MigrationStateRolledBack
	migration.RolledBackAt = &startTime
	migration.UpdatedAt = time.Now()

	// Log rollback completion
	e.logRollbackComplete(ctx, migration)

	return nil
}

// ValidateRollback validates a rollback before execution (implements MigrationExecutor interface)
func (e *DatabaseMigrationExecutor) ValidateRollback(ctx context.Context, migration *Migration) error {
	// Check if migration content is not empty
	if strings.TrimSpace(migration.Script) == "" {
		return fmt.Errorf("migration %s has empty content", migration.ID)
	}

	// Validate SQL syntax (basic check)
	if err := e.validateSQL(migration.Script); err != nil {
		return fmt.Errorf("invalid SQL in migration %s: %w", migration.ID, err)
	}

	// Check for dangerous operations if in production
	if e.config.Environment == EnvironmentProduction {
		if err := e.checkDangerousOperations(migration.Script); err != nil {
			return fmt.Errorf("dangerous operation in migration %s: %w", migration.ID, err)
		}
	}

	return nil
}

// ValidateMigration validates a migration before execution (implements MigrationExecutor interface)
func (e *DatabaseMigrationExecutor) ValidateMigration(ctx context.Context, migration *Migration) error {
	// Check if migration content is not empty
	if strings.TrimSpace(migration.Script) == "" {
		return fmt.Errorf("migration %s has empty content", migration.ID)
	}

	// Validate SQL syntax (basic check)
	if err := e.validateSQL(migration.Script); err != nil {
		return fmt.Errorf("invalid SQL in migration %s: %w", migration.ID, err)
	}

	// Check for dangerous operations if in production
	if e.config.Environment == EnvironmentProduction {
		if err := e.checkDangerousOperations(migration.Script); err != nil {
			return fmt.Errorf("dangerous operation in migration %s: %w", migration.ID, err)
		}
	}

	return nil
}

// GetExecutionPlan returns the execution plan for migrations
func (e *DatabaseMigrationExecutor) GetExecutionPlan(migrations []*Migration) ([]string, error) {
	plan := make([]string, 0, len(migrations))

	for _, migration := range migrations {
		planItem := fmt.Sprintf("Execute migration %s: %s", migration.Version, migration.Description)
		plan = append(plan, planItem)
	}

	return plan, nil
}

// executeSQL executes SQL statements
func (e *DatabaseMigrationExecutor) executeSQL(ctx context.Context, sqlContent string) error {
	// Split SQL content into individual statements
	statements := e.splitSQLStatements(sqlContent)

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Execute the statement
		if err := e.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("failed to execute SQL statement: %s, error: %w", stmt, err)
		}
	}

	return nil
}

// splitSQLStatements splits SQL content into individual statements
func (e *DatabaseMigrationExecutor) splitSQLStatements(sqlContent string) []string {
	// Simple statement splitting by semicolon
	// In a production system, this would need to handle more complex cases
	statements := strings.Split(sqlContent, ";")

	result := make([]string, 0, len(statements))
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			result = append(result, stmt)
		}
	}

	return result
}

// validateSQL performs basic SQL validation
func (e *DatabaseMigrationExecutor) validateSQL(sqlContent string) error {
	// Basic validation - check for balanced parentheses, quotes, etc.

	// Check for balanced parentheses
	parenCount := 0
	singleQuoteCount := 0
	doubleQuoteCount := 0

	for _, char := range sqlContent {
		switch char {
		case '(':
			parenCount++
		case ')':
			parenCount--
		case '\'':
			singleQuoteCount++
		case '"':
			doubleQuoteCount++
		}
	}

	if parenCount != 0 {
		return fmt.Errorf("unbalanced parentheses in SQL")
	}

	if singleQuoteCount%2 != 0 {
		return fmt.Errorf("unbalanced single quotes in SQL")
	}

	if doubleQuoteCount%2 != 0 {
		return fmt.Errorf("unbalanced double quotes in SQL")
	}

	return nil
}

// checkDangerousOperations checks for dangerous SQL operations
func (e *DatabaseMigrationExecutor) checkDangerousOperations(sqlContent string) error {
	dangerousPatterns := []string{
		"DROP DATABASE",
		"TRUNCATE TABLE",
		"DELETE FROM",
		"DROP SCHEMA",
		"ALTER SYSTEM",
	}

	upperSQL := strings.ToUpper(sqlContent)

	for _, pattern := range dangerousPatterns {
		if strings.Contains(upperSQL, pattern) {
			return fmt.Errorf("dangerous operation detected: %s", pattern)
		}
	}

	return nil
}

// logMigrationStart logs the start of a migration
func (e *DatabaseMigrationExecutor) logMigrationStart(ctx context.Context, migration *Migration) {
	if e.config.Verbose {
		e.logger.WithContext(ctx).Info("Starting migration", "version", migration.Version, "description", migration.Description, "id", migration.ID)
	}
}

// logMigrationComplete logs the completion of a migration
func (e *DatabaseMigrationExecutor) logMigrationComplete(ctx context.Context, migration *Migration) {
	if e.config.Verbose {
		e.logger.WithContext(ctx).Info("Completed migration", "version", migration.Version, "execution_time", migration.ExecutionTime, "id", migration.ID)
	}
}

// logRollbackStart logs the start of a rollback
func (e *DatabaseMigrationExecutor) logRollbackStart(ctx context.Context, migration *Migration) {
	if e.config.Verbose {
		e.logger.WithContext(ctx).Info("Starting rollback of migration", "version", migration.Version, "description", migration.Description, "id", migration.ID)
	}
}

// logRollbackComplete logs the completion of a rollback
func (e *DatabaseMigrationExecutor) logRollbackComplete(ctx context.Context, migration *Migration) {
	if e.config.Verbose {
		e.logger.WithContext(ctx).Info("Completed rollback of migration", "version", migration.Version, "execution_time", migration.ExecutionTime, "id", migration.ID)
	}
}

// Transaction Executor Implementation

// Execute executes a migration within a transaction (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) Execute(ctx context.Context, migration *Migration) error {
	return e.db.Transaction(func(tx *gorm.DB) error {
		return e.executor.Execute(ctx, migration)
	})
}

// ExecuteDryRun simulates migration execution (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error) {
	return e.executor.ExecuteDryRun(ctx, migration)
}

// Rollback rolls back a migration within a transaction (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) Rollback(ctx context.Context, migration *Migration) error {
	return e.db.Transaction(func(tx *gorm.DB) error {
		return e.executor.Rollback(ctx, migration)
	})
}

// ValidateRollback validates a rollback (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) ValidateRollback(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateRollback(ctx, migration)
}

// ValidateMigration validates a migration (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) ValidateMigration(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateMigration(ctx, migration)
}

// GetExecutionPlan returns the execution plan (implements MigrationExecutor interface)
func (e *TransactionMigrationExecutor) GetExecutionPlan(migrations []*Migration) ([]string, error) {
	return e.executor.GetExecutionPlan(migrations)
}

// Dry Run Executor Implementation

// Execute simulates migration execution (implements MigrationExecutor interface)
func (e *DryRunMigrationExecutor) Execute(ctx context.Context, migration *Migration) error {
	// Generate detailed analysis
	plan, err := e.analyzer.AnalyzeMigration(ctx, migration)
	if err != nil {
		e.output = append(e.output, fmt.Sprintf("Failed to analyze migration %s: %v", migration.Version, err))
		return err
	}

	// Format and store detailed output
	detailedOutput := e.analyzer.FormatDetailedPlan(plan)
	e.output = append(e.output, detailedOutput)

	// Simulate execution time based on analysis
	migration.State = "simulated"
	now := time.Now()
	migration.ExecutedAt = &now
	migration.ExecutionTime = plan.Summary.EstimatedDuration
	migration.UpdatedAt = time.Now()

	return nil
}

// ExecuteDryRun returns execution plan (implements MigrationExecutor interface)
func (e *DryRunMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error) {
	// Generate detailed analysis
	detailedPlan, err := e.analyzer.AnalyzeMigration(ctx, migration)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze migration: %w", err)
	}

	// Create enhanced execution plan
	plan := &ExecutionPlan{
		Migrations:    []*Migration{migration},
		TotalCount:    1,
		EstimatedTime: detailedPlan.Summary.EstimatedDuration,
		Warnings:      detailedPlan.Warnings,
		Errors:        detailedPlan.Errors,
	}

	return plan, nil
}

// Rollback simulates migration rollback (implements MigrationExecutor interface)
func (e *DryRunMigrationExecutor) Rollback(ctx context.Context, migration *Migration) error {
	if !migration.CanRollback {
		return fmt.Errorf("migration %s cannot be rolled back", migration.ID)
	}

	// Analyze rollback script if available
	if migration.RollbackSQL != "" {
		// Create a temporary migration for rollback analysis
		rollbackMigration := &Migration{
			ID:          migration.ID + "_rollback",
			Version:     migration.Version + "_rollback",
			Description: "Rollback of " + migration.Description,
			Script:      migration.RollbackSQL,
			Type:        migration.Type,
		}

		plan, err := e.analyzer.AnalyzeMigration(ctx, rollbackMigration)
		if err != nil {
			e.output = append(e.output, fmt.Sprintf("Failed to analyze rollback for migration %s: %v", migration.Version, err))
		} else {
			e.output = append(e.output, "=== ROLLBACK ANALYSIS ===")
			detailedOutput := e.analyzer.FormatDetailedPlan(plan)
			e.output = append(e.output, detailedOutput)
		}
	} else {
		e.output = append(e.output, fmt.Sprintf("Would rollback migration %s: %s", migration.Version, migration.Description))
		e.output = append(e.output, "⚠️  No rollback script provided - manual rollback required")
	}

	migration.State = "rollback_simulated"
	now := time.Now()
	migration.RolledBackAt = &now
	migration.UpdatedAt = time.Now()

	return nil
}

// ValidateRollback validates a rollback (implements MigrationExecutor interface)
func (e *DryRunMigrationExecutor) ValidateRollback(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateRollback(ctx, migration)
}

// ValidateMigration validates a migration
func (e *DryRunMigrationExecutor) ValidateMigration(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateMigration(ctx, migration)
}

// GetExecutionPlan returns the execution plan
func (e *DryRunMigrationExecutor) GetExecutionPlan(migrations []*Migration) ([]string, error) {
	plan, err := e.executor.GetExecutionPlan(migrations)
	if err != nil {
		return nil, err
	}

	// Add dry run prefix
	dryRunPlan := make([]string, len(plan))
	for i, item := range plan {
		dryRunPlan[i] = "[DRY RUN] " + item
	}

	return dryRunPlan, nil
}

// GetOutput returns the dry run output
func (e *DryRunMigrationExecutor) GetOutput() []string {
	return e.output
}

// ClearOutput clears the dry run output
func (e *DryRunMigrationExecutor) ClearOutput() {
	e.output = make([]string, 0)
}

// GetDetailedAnalysis returns detailed analysis for a migration
func (e *DryRunMigrationExecutor) GetDetailedAnalysis(ctx context.Context, migration *Migration) (*DetailedExecutionPlan, error) {
	return e.analyzer.AnalyzeMigration(ctx, migration)
}

// GetFormattedAnalysis returns formatted analysis output for a migration
func (e *DryRunMigrationExecutor) GetFormattedAnalysis(ctx context.Context, migration *Migration) (string, error) {
	plan, err := e.analyzer.AnalyzeMigration(ctx, migration)
	if err != nil {
		return "", err
	}
	return e.analyzer.FormatDetailedPlan(plan), nil
}

// BatchMigrationExecutor executes multiple migrations in batches
type BatchMigrationExecutor struct {
	executor  MigrationExecutor
	batchSize int
}

// NewBatchMigrationExecutor creates a new batch migration executor
func NewBatchMigrationExecutor(executor MigrationExecutor, batchSize int) *BatchMigrationExecutor {
	return &BatchMigrationExecutor{
		executor:  executor,
		batchSize: batchSize,
	}
}

// ExecuteMigrations executes multiple migrations in batches
func (e *BatchMigrationExecutor) ExecuteMigrations(ctx context.Context, migrations []*Migration) error {
	for i := 0; i < len(migrations); i += e.batchSize {
		end := i + e.batchSize
		if end > len(migrations) {
			end = len(migrations)
		}

		batch := migrations[i:end]

		for _, migration := range batch {
			if err := e.executor.Execute(ctx, migration); err != nil {
				return fmt.Errorf("failed to execute migration batch starting at index %d: %w", i, err)
			}
		}

		// Optional: Add delay between batches
		if i+e.batchSize < len(migrations) {
			time.Sleep(time.Millisecond * 100)
		}
	}

	return nil
}

// ParallelMigrationExecutor executes migrations in parallel (use with caution)
type ParallelMigrationExecutor struct {
	executor    MigrationExecutor
	concurrency int
}

// NewParallelMigrationExecutor creates a new parallel migration executor
func NewParallelMigrationExecutor(executor MigrationExecutor, concurrency int) *ParallelMigrationExecutor {
	return &ParallelMigrationExecutor{
		executor:    executor,
		concurrency: concurrency,
	}
}

// ExecuteMigrations executes migrations in parallel
func (e *ParallelMigrationExecutor) ExecuteMigrations(ctx context.Context, migrations []*Migration) error {
	// Note: This should only be used for independent migrations
	// Most migrations should be executed sequentially

	semaphore := make(chan struct{}, e.concurrency)
	errChan := make(chan error, len(migrations))

	for _, migration := range migrations {
		go func(m *Migration) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := e.executor.Execute(ctx, m); err != nil {
				errChan <- err
				return
			}
			errChan <- nil
		}(migration)
	}

	// Wait for all migrations to complete
	for i := 0; i < len(migrations); i++ {
		if err := <-errChan; err != nil {
			return err
		}
	}

	return nil
}

// ConditionalMigrationExecutor executes migrations based on conditions
type ConditionalMigrationExecutor struct {
	executor   MigrationExecutor
	conditions map[string]func(*Migration) bool
}

// NewConditionalMigrationExecutor creates a new conditional migration executor
func NewConditionalMigrationExecutor(executor MigrationExecutor) *ConditionalMigrationExecutor {
	return &ConditionalMigrationExecutor{
		executor:   executor,
		conditions: make(map[string]func(*Migration) bool),
	}
}

// AddCondition adds a condition for migration execution
func (e *ConditionalMigrationExecutor) AddCondition(name string, condition func(*Migration) bool) {
	e.conditions[name] = condition
}

// Execute executes a migration if all conditions are met (implements MigrationExecutor interface)
func (e *ConditionalMigrationExecutor) Execute(ctx context.Context, migration *Migration) error {
	// Check all conditions
	for name, condition := range e.conditions {
		if !condition(migration) {
			return fmt.Errorf("migration %s failed condition: %s", migration.ID, name)
		}
	}

	return e.executor.Execute(ctx, migration)
}

// ExecuteDryRun simulates migration execution (implements MigrationExecutor interface)
func (e *ConditionalMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error) {
	// Check all conditions
	for name, condition := range e.conditions {
		if !condition(migration) {
			return nil, fmt.Errorf("migration %s failed condition: %s", migration.ID, name)
		}
	}

	return e.executor.ExecuteDryRun(ctx, migration)
}

// Rollback rolls back a migration if conditions allow (implements MigrationExecutor interface)
func (e *ConditionalMigrationExecutor) Rollback(ctx context.Context, migration *Migration) error {
	// Check all conditions
	for name, condition := range e.conditions {
		if !condition(migration) {
			return fmt.Errorf("migration %s rollback failed condition: %s", migration.ID, name)
		}
	}

	return e.executor.Rollback(ctx, migration)
}

// ValidateRollback validates a rollback (implements MigrationExecutor interface)
func (e *ConditionalMigrationExecutor) ValidateRollback(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateRollback(ctx, migration)
}

// ExecuteMigration executes a migration if all conditions are met
func (e *ConditionalMigrationExecutor) ExecuteMigration(ctx context.Context, migration *Migration) error {
	// Check all conditions
	for name, condition := range e.conditions {
		if !condition(migration) {
			return fmt.Errorf("migration %s failed condition: %s", migration.ID, name)
		}
	}

	return e.executor.Execute(ctx, migration)
}

// ValidateMigration validates a migration
func (e *ConditionalMigrationExecutor) ValidateMigration(ctx context.Context, migration *Migration) error {
	return e.executor.ValidateMigration(ctx, migration)
}

// GetExecutionPlan returns the execution plan
func (e *ConditionalMigrationExecutor) GetExecutionPlan(migrations []*Migration) ([]string, error) {
	return e.executor.GetExecutionPlan(migrations)
}
