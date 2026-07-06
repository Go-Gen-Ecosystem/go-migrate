/**
 * @file components.go
 * @description DB2-specific components: lock manager, validator, and resolver
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/db2/components.go
 */

package db2

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// DB2LockManager implements LockManager for IBM DB2
type DB2LockManager struct {
	config    types.Config
	db        *sql.DB
	tableName string
	options   map[string]interface{}
}

// AcquireLock acquires a migration lock in DB2
func (lm *DB2LockManager) AcquireLock(ctx context.Context, lockInfo types.LockInfo) error {
	if lm.db == nil {
		return fmt.Errorf("database connection not established")
	}

	// Ensure lock table exists
	if err := lm.ensureLockTable(ctx); err != nil {
		return fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Try to acquire lock using DB2's advisory lock mechanism
	query := fmt.Sprintf(`
		INSERT INTO %s (lock_id, owner, operation, acquired_at, expires_at)
		VALUES (?, ?, ?, CURRENT TIMESTAMP, CURRENT TIMESTAMP + %d SECONDS)
	`, lm.tableName, lm.getLockTimeout())

	_, err := lm.db.ExecContext(ctx, query, lockInfo.ID, lockInfo.Owner, lockInfo.Operation)
	if err != nil {
		// Check if it's a duplicate key error (lock already exists)
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate") {
			return fmt.Errorf("migration lock %s is already held", lockInfo.ID)
		}
		return fmt.Errorf("failed to acquire lock %s: %w", lockInfo.ID, err)
	}

	return nil
}

// ReleaseLock releases a migration lock in DB2
func (lm *DB2LockManager) ReleaseLock(ctx context.Context, lockID string) error {
	if lm.db == nil {
		return fmt.Errorf("database connection not established")
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE lock_id = ?`, lm.tableName)

	result, err := lm.db.ExecContext(ctx, query, lockID)
	if err != nil {
		return fmt.Errorf("failed to release lock %s: %w", lockID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for lock %s: %w", lockID, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("lock %s not found or already released", lockID)
	}

	return nil
}

// IsLocked checks if a migration lock exists in DB2
func (lm *DB2LockManager) IsLocked(ctx context.Context, lockID string) (bool, error) {
	if lm.db == nil {
		return false, fmt.Errorf("database connection not established")
	}

	// Ensure lock table exists
	if err := lm.ensureLockTable(ctx); err != nil {
		return false, fmt.Errorf("failed to ensure lock table: %w", err)
	}

	// Clean up expired locks first
	lm.cleanupExpiredLocks(ctx)

	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s 
		WHERE lock_id = ? AND expires_at > CURRENT TIMESTAMP
	`, lm.tableName)

	var count int
	err := lm.db.QueryRowContext(ctx, query, lockID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// ensureLockTable creates the lock table if it doesn't exist
func (lm *DB2LockManager) ensureLockTable(ctx context.Context) error {
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			lock_id VARCHAR(255) NOT NULL PRIMARY KEY,
			owner VARCHAR(255) NOT NULL,
			operation VARCHAR(100) NOT NULL,
			acquired_at TIMESTAMP NOT NULL DEFAULT CURRENT TIMESTAMP,
			expires_at TIMESTAMP NOT NULL
		)
	`, lm.tableName)

	_, err := lm.db.ExecContext(ctx, createTableSQL)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create lock table: %w", err)
	}

	return nil
}

// cleanupExpiredLocks removes expired locks
func (lm *DB2LockManager) cleanupExpiredLocks(ctx context.Context) {
	query := fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= CURRENT TIMESTAMP`, lm.tableName)
	lm.db.ExecContext(ctx, query)
}

// getLockTimeout returns the lock timeout in seconds
func (lm *DB2LockManager) getLockTimeout() int {
	if timeout, ok := lm.options["lock_timeout"].(int); ok {
		return timeout
	}
	return 3600 // 1 hour default
}

// DB2Validator implements MigrationValidator for IBM DB2
type DB2Validator struct {
	config  types.Config
	db      *sql.DB
	options map[string]interface{}
}

// Validate validates migrations for DB2 compatibility
func (v *DB2Validator) Validate(ctx context.Context, migrations []types.MigrationScript) (types.ValidationResult, error) {
	result := types.ValidationResult{
		Valid:        true,
		Errors:       make([]string, 0),
		Warnings:     make([]string, 0),
		ErrorCount:   0,
		WarningCount: 0,
	}

	for _, migration := range migrations {
		// Validate migration structure
		if err := v.validateMigrationStructure(migration); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Migration %s: %v", migration.Version, err))
			result.Valid = false
		}

		// Validate SQL syntax for DB2
		if err := v.validateDB2SQL(migration.UpScript); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Migration %s up script: %v", migration.Version, err))
			result.Valid = false
		}

		// Validate rollback script if present
		if migration.DownScript != "" {
			if err := v.validateDB2SQL(migration.DownScript); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Migration %s down script: %v", migration.Version, err))
				result.Valid = false
			}
		} else {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Migration %s has no rollback script", migration.Version))
		}

		// Check for DB2-specific best practices
		warnings := v.checkDB2BestPractices(migration)
		result.Warnings = append(result.Warnings, warnings...)
	}

	// Validate migration order and dependencies
	if err := v.validateMigrationOrder(migrations); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Migration order validation: %v", err))
		result.Valid = false
	}

	result.ErrorCount = len(result.Errors)
	result.WarningCount = len(result.Warnings)

	return result, nil
}

// validateMigrationStructure validates the basic structure of a migration
func (v *DB2Validator) validateMigrationStructure(migration types.MigrationScript) error {
	if migration.Version == "" {
		return fmt.Errorf("migration version is required")
	}

	if migration.UpScript == "" {
		return fmt.Errorf("migration up script is required")
	}

	if migration.Description == "" {
		return fmt.Errorf("migration description is required")
	}

	// Validate version format (should be semantic version or timestamp)
	if !v.isValidVersionFormat(migration.Version) {
		return fmt.Errorf("invalid version format: %s", migration.Version)
	}

	return nil
}

// validateDB2SQL validates SQL for DB2 compatibility
func (v *DB2Validator) validateDB2SQL(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("empty SQL script")
	}

	upperSQL := strings.ToUpper(sql)

	// Check for unsupported MySQL/PostgreSQL syntax
	if strings.Contains(upperSQL, "AUTO_INCREMENT") {
		return fmt.Errorf("use GENERATED ALWAYS AS IDENTITY instead of AUTO_INCREMENT for DB2")
	}

	if strings.Contains(upperSQL, "SERIAL") {
		return fmt.Errorf("SERIAL type is not supported in DB2, use INTEGER with GENERATED ALWAYS AS IDENTITY")
	}

	// Check for proper DB2 syntax
	if strings.Contains(upperSQL, "LIMIT") && !strings.Contains(upperSQL, "FETCH FIRST") {
		return fmt.Errorf("use FETCH FIRST n ROWS ONLY instead of LIMIT for DB2")
	}

	// Validate table and column names (DB2 has specific naming rules)
	if err := v.validateDB2Names(sql); err != nil {
		return err
	}

	return nil
}

// validateDB2Names validates table and column names for DB2 compliance
func (v *DB2Validator) validateDB2Names(sql string) error {
	// DB2 identifier rules: max 128 characters, must start with letter or @, #, $
	identifierPattern := regexp.MustCompile(`\b[A-Za-z@#$][A-Za-z0-9@#$_]*\b`)
	
	// This is a simplified validation - in production, you'd want proper SQL parsing
	words := identifierPattern.FindAllString(sql, -1)
	
	for _, word := range words {
		if len(word) > 128 {
			return fmt.Errorf("identifier '%s' exceeds DB2 maximum length of 128 characters", word)
		}
		
		// Check for reserved words (simplified list)
		if v.isDB2ReservedWord(strings.ToUpper(word)) {
			return fmt.Errorf("'%s' is a DB2 reserved word and should be quoted", word)
		}
	}
	
	return nil
}

// isDB2ReservedWord checks if a word is a DB2 reserved word
func (v *DB2Validator) isDB2ReservedWord(word string) bool {
	reservedWords := map[string]bool{
		"ADD": true, "ALL": true, "ALTER": true, "AND": true, "ANY": true,
		"AS": true, "ASC": true, "BETWEEN": true, "BY": true, "CASE": true,
		"CHECK": true, "COLUMN": true, "CONSTRAINT": true, "CREATE": true,
		"DELETE": true, "DESC": true, "DISTINCT": true, "DROP": true,
		"EXISTS": true, "FROM": true, "GROUP": true, "HAVING": true,
		"IN": true, "INDEX": true, "INSERT": true, "INTO": true, "IS": true,
		"JOIN": true, "KEY": true, "LIKE": true, "NOT": true, "NULL": true,
		"ON": true, "OR": true, "ORDER": true, "PRIMARY": true, "SELECT": true,
		"SET": true, "TABLE": true, "UNION": true, "UNIQUE": true, "UPDATE": true,
		"VALUES": true, "WHERE": true, "WITH": true,
	}
	
	return reservedWords[word]
}

// checkDB2BestPractices checks for DB2-specific best practices
func (v *DB2Validator) checkDB2BestPractices(migration types.MigrationScript) []string {
	var warnings []string
	upperSQL := strings.ToUpper(migration.UpScript)

	// Check for missing indexes on foreign keys
	if strings.Contains(upperSQL, "FOREIGN KEY") && !strings.Contains(upperSQL, "CREATE INDEX") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: Consider creating indexes on foreign key columns for better performance", migration.Version))
	}

	// Check for large table operations without proper considerations
	if strings.Contains(upperSQL, "ALTER TABLE") && strings.Contains(upperSQL, "ADD COLUMN") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: ALTER TABLE ADD COLUMN on large tables may cause performance issues", migration.Version))
	}

	// Check for missing table spaces specification
	if strings.Contains(upperSQL, "CREATE TABLE") && !strings.Contains(upperSQL, "IN ") {
		warnings = append(warnings, fmt.Sprintf("Migration %s: Consider specifying table space for better performance", migration.Version))
	}

	return warnings
}

// validateMigrationOrder validates that migrations are in correct order
func (v *DB2Validator) validateMigrationOrder(migrations []types.MigrationScript) error {
	if len(migrations) <= 1 {
		return nil
	}

	// Sort migrations by version to check order
	sortedMigrations := make([]types.MigrationScript, len(migrations))
	copy(sortedMigrations, migrations)
	
	sort.Slice(sortedMigrations, func(i, j int) bool {
		return sortedMigrations[i].Version < sortedMigrations[j].Version
	})

	// Check if original order matches sorted order
	for i, migration := range migrations {
		if migration.Version != sortedMigrations[i].Version {
			return fmt.Errorf("migrations are not in correct order: %s should come before %s", 
				sortedMigrations[i].Version, migration.Version)
		}
	}

	return nil
}

// isValidVersionFormat validates the version format
func (v *DB2Validator) isValidVersionFormat(version string) bool {
	// Accept semantic version (x.y.z) or timestamp (YYYYMMDDHHMMSS) format
	semverPattern := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	timestampPattern := regexp.MustCompile(`^\d{14}$`)
	
	return semverPattern.MatchString(version) || timestampPattern.MatchString(version)
}

// DB2Resolver implements DependencyResolver for IBM DB2
type DB2Resolver struct {
	config  types.Config
	options map[string]interface{}
}

// ResolveDependencies resolves migration dependencies for DB2
func (r *DB2Resolver) ResolveDependencies(ctx context.Context, migrations []types.MigrationScript) (types.ExecutionPlan, error) {
	plan := types.ExecutionPlan{
		Migrations:  make([]types.MigrationScript, 0),
		Phases:      make([]types.ExecutionPhase, 0),
		Constraints: make([]types.DependencyConstraint, 0),
	}

	if len(migrations) == 0 {
		return plan, nil
	}

	// Build dependency graph
	dependencyGraph := r.buildDependencyGraph(migrations)

	// Perform topological sort to resolve dependencies
	sortedMigrations, err := r.topologicalSort(migrations, dependencyGraph)
	if err != nil {
		return plan, fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	// Create execution phases
	phases := r.createExecutionPhases(sortedMigrations)

	plan.Migrations = sortedMigrations
	plan.Phases = phases
	plan.Constraints = r.extractConstraints(migrations)

	return plan, nil
}

// buildDependencyGraph builds a dependency graph from migrations
func (r *DB2Resolver) buildDependencyGraph(migrations []types.MigrationScript) map[string][]string {
	graph := make(map[string][]string)

	for _, migration := range migrations {
		graph[migration.Version] = make([]string, 0)

		// Extract dependencies from migration metadata or comments
		dependencies := r.extractDependencies(migration)
		graph[migration.Version] = dependencies
	}

	return graph
}

// extractDependencies extracts dependencies from migration
func (r *DB2Resolver) extractDependencies(migration types.MigrationScript) []string {
	var dependencies []string

	// Look for dependency comments in the script
	lines := strings.Split(migration.UpScript, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-- DEPENDS ON:") {
			depLine := strings.TrimPrefix(line, "-- DEPENDS ON:")
			deps := strings.Split(depLine, ",")
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					dependencies = append(dependencies, dep)
				}
			}
		}
	}

	// Implicit dependencies based on version order
	// Migrations depend on all previous migrations by default
	for _, otherMigration := range r.getAllMigrations() {
		if otherMigration.Version < migration.Version {
			dependencies = append(dependencies, otherMigration.Version)
		}
	}

	return dependencies
}

// topologicalSort performs topological sort on the dependency graph
func (r *DB2Resolver) topologicalSort(migrations []types.MigrationScript, graph map[string][]string) ([]types.MigrationScript, error) {
	// Kahn's algorithm for topological sorting
	inDegree := make(map[string]int)
	migrationMap := make(map[string]types.MigrationScript)

	// Initialize
	for _, migration := range migrations {
		inDegree[migration.Version] = 0
		migrationMap[migration.Version] = migration
	}

	// Calculate in-degrees
	for _, deps := range graph {
		for _, dep := range deps {
			if _, exists := inDegree[dep]; exists {
				inDegree[dep]++
			}
		}
	}

	// Find nodes with no incoming edges
	queue := make([]string, 0)
	for version, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, version)
		}
	}

	var result []types.MigrationScript
	for len(queue) > 0 {
		// Remove a node from queue
		current := queue[0]
		queue = queue[1:]
		
		result = append(result, migrationMap[current])

		// For each neighbor of current node
		for _, neighbor := range graph[current] {
			if _, exists := inDegree[neighbor]; exists {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					queue = append(queue, neighbor)
				}
			}
		}
	}

	// Check for cycles
	if len(result) != len(migrations) {
		return nil, fmt.Errorf("circular dependency detected in migrations")
	}

	return result, nil
}

// createExecutionPhases creates execution phases for parallel execution
func (r *DB2Resolver) createExecutionPhases(migrations []types.MigrationScript) []types.ExecutionPhase {
	var phases []types.ExecutionPhase

	// For DB2, we typically execute migrations sequentially due to schema dependencies
	// But we can group independent migrations together
	currentPhase := types.ExecutionPhase{
		Name:        "sequential",
		Migrations:  make([]types.MigrationScript, 0),
		CanParallel: false,
	}

	for _, migration := range migrations {
		currentPhase.Migrations = append(currentPhase.Migrations, migration)
	}

	phases = append(phases, currentPhase)
	return phases
}

// extractConstraints extracts dependency constraints
func (r *DB2Resolver) extractConstraints(migrations []types.MigrationScript) []types.DependencyConstraint {
	var constraints []types.DependencyConstraint

	for _, migration := range migrations {
		// Extract constraints from migration metadata
		if strings.Contains(migration.UpScript, "-- CONSTRAINT:") {
			constraint := types.DependencyConstraint{
				Type:        "order",
				Source:      migration.Version,
				Target:      "", // Will be filled based on constraint type
				Description: fmt.Sprintf("Migration %s has ordering constraints", migration.Version),
			}
			constraints = append(constraints, constraint)
		}
	}

	return constraints
}

// getAllMigrations returns all migrations (placeholder for actual implementation)
func (r *DB2Resolver) getAllMigrations() []types.MigrationScript {
	// This would typically come from a migration registry or loader
	return make([]types.MigrationScript, 0)
}

// Helper function to calculate checksum for migrations
func calculateChecksum(content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", hash)
}
