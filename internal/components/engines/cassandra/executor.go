/**
 * @file executor.go
 * @description Cassandra migration executor implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/cassandra/executor.go
 */

package cassandra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// CassandraMigrationExecutor implements NoSQLMigrationExecutor for Cassandra
type CassandraMigrationExecutor struct {
	session *gocql.Session
	cluster *gocql.ClusterConfig
	config  *types.NoSQLConnectionConfig
}

// NewCassandraMigrationExecutor creates a new Cassandra migration executor
func NewCassandraMigrationExecutor(config *types.NoSQLConnectionConfig) *CassandraMigrationExecutor {
	cluster := gocql.NewCluster(config.Hosts...)
	cluster.Port = config.Port
	cluster.Keyspace = config.Keyspace
	cluster.Consistency = gocql.Quorum
	cluster.Timeout = config.ConnectTimeout
	cluster.ConnectTimeout = config.ConnectTimeout
	cluster.NumConns = config.MaxConnections

	if config.Username != "" && config.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: config.Username,
			Password: config.Password,
		}
	}

	if config.SSL {
		cluster.SslOpts = &gocql.SslOptions{
			EnableHostVerification: false,
		}
		if config.SSLCertPath != "" {
			cluster.SslOpts.CertPath = config.SSLCertPath
		}
		if config.SSLKeyPath != "" {
			cluster.SslOpts.KeyPath = config.SSLKeyPath
		}
		if config.SSLCAPath != "" {
			cluster.SslOpts.CaPath = config.SSLCAPath
		}
	}

	return &CassandraMigrationExecutor{
		config:  config,
		cluster: cluster,
	}
}

// Connect establishes connection to Cassandra
func (e *CassandraMigrationExecutor) Connect(ctx context.Context) error {
	session, err := e.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to connect to Cassandra: %w", err)
	}
	e.session = session
	return nil
}

// Disconnect closes the Cassandra connection
func (e *CassandraMigrationExecutor) Disconnect(ctx context.Context) error {
	if e.session != nil {
		e.session.Close()
		e.session = nil
	}
	return nil
}

// Ping checks the connection to Cassandra
func (e *CassandraMigrationExecutor) Ping(ctx context.Context) error {
	if e.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	query := e.session.Query("SELECT now() FROM system.local")
	defer query.Release()

	var now time.Time
	if err := query.Scan(&now); err != nil {
		return fmt.Errorf("failed to ping Cassandra: %w", err)
	}

	return nil
}

// GetDatabaseInfo returns information about the Cassandra database
func (e *CassandraMigrationExecutor) GetDatabaseInfo(ctx context.Context) (*types.NoSQLDatabaseInfo, error) {
	if e.session == nil {
		return nil, fmt.Errorf("not connected to Cassandra")
	}

	// Get cluster information
	query := e.session.Query("SELECT cluster_name, release_version FROM system.local")
	defer query.Release()

	var clusterName, version string
	if err := query.Scan(&clusterName, &version); err != nil {
		return nil, fmt.Errorf("failed to get Cassandra info: %w", err)
	}

	// Get keyspace information
	keyspaceQuery := e.session.Query("SELECT keyspace_name FROM system_schema.keyspaces WHERE keyspace_name = ?", e.config.Keyspace)
	defer keyspaceQuery.Release()

	var keyspace string
	keyspaceExists := keyspaceQuery.Scan(&keyspace) == nil

	info := &types.NoSQLDatabaseInfo{
		Type:     "cassandra",
		Version:  version,
		Host:     strings.Join(e.config.Hosts, ","),
		Port:     e.config.Port,
		Keyspace: e.config.Keyspace,
		Features: map[string]bool{
			"keyspace_exists": keyspaceExists,
			"ssl_enabled":     e.config.SSL,
			"auth_enabled":    e.config.Username != "",
		},
		Configuration: map[string]string{
			"cluster_name": clusterName,
			"consistency":  "QUORUM",
		},
		Status:      "connected",
		LastChecked: time.Now(),
	}

	return info, nil
}

// Execute executes a Cassandra migration
func (e *CassandraMigrationExecutor) Execute(ctx context.Context, migration *types.Migration) error {
	if e.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	startTime := time.Now()

	// Parse and execute CQL statements
	statements := e.parseCQLStatements(migration.Script)

	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		query := e.session.Query(stmt)
		if err := query.Exec(); err != nil {
			migration.State = "failed"
			migration.ErrorMessage = fmt.Sprintf("failed to execute statement %d: %v", i+1, err)
			return fmt.Errorf("failed to execute CQL statement %d: %w", i+1, err)
		}
		query.Release()
	}

	// Update migration state
	executedAt := time.Now()
	migration.State = "applied"
	migration.ExecutedAt = &executedAt
	migration.ExecutionTime = time.Since(startTime)
	migration.UpdatedAt = time.Now()

	return nil
}

// ExecuteDryRun simulates migration execution without applying changes
func (e *CassandraMigrationExecutor) ExecuteDryRun(ctx context.Context, migration *types.Migration) (*types.ExecutionPlan, error) {
	statements := e.parseCQLStatements(migration.Script)

	plan := &types.ExecutionPlan{
		Migrations:    []*types.Migration{migration},
		TotalCount:    1,
		EstimatedTime: time.Duration(len(statements)) * 100 * time.Millisecond, // Rough estimate
		Warnings:      []string{},
		Errors:        []string{},
	}

	// Validate each statement
	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		if err := e.validateCQLStatement(stmt); err != nil {
			plan.Errors = append(plan.Errors, fmt.Sprintf("Statement %d: %v", i+1, err))
		}

		// Check for potentially dangerous operations
		if e.isDangerousOperation(stmt) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Statement %d contains potentially dangerous operation: %s", i+1, stmt))
		}
	}

	return plan, nil
}

// Rollback executes rollback for a Cassandra migration
func (e *CassandraMigrationExecutor) Rollback(ctx context.Context, migration *types.Migration) error {
	if e.session == nil {
		return fmt.Errorf("not connected to Cassandra")
	}

	if migration.RollbackSQL == "" {
		return fmt.Errorf("no rollback script available for migration %s", migration.ID)
	}

	startTime := time.Now()

	// Parse and execute rollback CQL statements
	statements := e.parseCQLStatements(migration.RollbackSQL)

	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		query := e.session.Query(stmt)
		if err := query.Exec(); err != nil {
			return fmt.Errorf("failed to execute rollback statement %d: %w", i+1, err)
		}
		query.Release()
	}

	// Update migration state
	rolledBackAt := time.Now()
	migration.State = "rolled_back"
	migration.RolledBackAt = &rolledBackAt
	migration.ExecutionTime = time.Since(startTime)
	migration.UpdatedAt = time.Now()

	return nil
}

// ValidateRollback validates a rollback before execution
func (e *CassandraMigrationExecutor) ValidateRollback(ctx context.Context, migration *types.Migration) error {
	if strings.TrimSpace(migration.RollbackSQL) == "" {
		return fmt.Errorf("migration %s has empty rollback script", migration.ID)
	}

	statements := e.parseCQLStatements(migration.RollbackSQL)
	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		if err := e.validateCQLStatement(stmt); err != nil {
			return fmt.Errorf("invalid CQL in rollback statement %d: %w", i+1, err)
		}
	}

	return nil
}

// ValidateMigration validates a migration before execution
func (e *CassandraMigrationExecutor) ValidateMigration(ctx context.Context, migration *types.Migration) error {
	if strings.TrimSpace(migration.Script) == "" {
		return fmt.Errorf("migration %s has empty script", migration.ID)
	}

	statements := e.parseCQLStatements(migration.Script)
	for i, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}

		if err := e.validateCQLStatement(stmt); err != nil {
			return fmt.Errorf("invalid CQL in statement %d: %w", i+1, err)
		}
	}

	return nil
}

// GetExecutionPlan returns execution plan for multiple migrations
func (e *CassandraMigrationExecutor) GetExecutionPlan(migrations []*types.Migration) ([]string, error) {
	var plan []string

	for _, migration := range migrations {
		statements := e.parseCQLStatements(migration.Script)
		for _, stmt := range statements {
			if strings.TrimSpace(stmt) != "" {
				plan = append(plan, fmt.Sprintf("-- Migration: %s\n%s", migration.ID, stmt))
			}
		}
	}

	return plan, nil
}

// parseCQLStatements parses CQL script into individual statements
func (e *CassandraMigrationExecutor) parseCQLStatements(script string) []string {
	// Split by semicolon, but be careful about semicolons in strings
	var statements []string
	var current strings.Builder
	inString := false
	escaped := false

	for _, char := range script {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}

		if char == '\\' {
			escaped = true
			current.WriteRune(char)
			continue
		}

		if char == '\'' {
			inString = !inString
			current.WriteRune(char)
			continue
		}

		if char == ';' && !inString {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteRune(char)
	}

	// Add the last statement if any
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}

// validateCQLStatement performs basic CQL validation
func (e *CassandraMigrationExecutor) validateCQLStatement(stmt string) error {
	stmt = strings.TrimSpace(strings.ToUpper(stmt))

	if stmt == "" {
		return fmt.Errorf("empty statement")
	}

	// Check for valid CQL keywords
	validKeywords := []string{
		"CREATE", "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "SELECT",
		"USE", "GRANT", "REVOKE", "LIST", "DESCRIBE", "COPY", "TRUNCATE",
	}

	hasValidKeyword := false
	for _, keyword := range validKeywords {
		if strings.HasPrefix(stmt, keyword) {
			hasValidKeyword = true
			break
		}
	}

	if !hasValidKeyword {
		return fmt.Errorf("statement does not start with a valid CQL keyword")
	}

	return nil
}

// isDangerousOperation checks if the CQL statement is potentially dangerous
func (e *CassandraMigrationExecutor) isDangerousOperation(stmt string) bool {
	stmt = strings.TrimSpace(strings.ToUpper(stmt))

	dangerousPatterns := []string{
		"DROP KEYSPACE",
		"DROP TABLE",
		"TRUNCATE",
		"DELETE FROM",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(stmt, pattern) {
			return true
		}
	}

	return false
}
