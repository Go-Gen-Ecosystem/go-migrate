/**
 * @file interfaces.go
 * @description NoSQL-specific interfaces for migration support
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/nosql/interfaces.go
 */

package types

import (
	"context"
	"time"
)

// NoSQLMigrationExecutor defines the interface for executing NoSQL migrations
type NoSQLMigrationExecutor interface {
	Execute(ctx context.Context, migration *Migration) error
	ExecuteDryRun(ctx context.Context, migration *Migration) (*ExecutionPlan, error)
	Rollback(ctx context.Context, migration *Migration) error
	ValidateRollback(ctx context.Context, migration *Migration) error
	ValidateMigration(ctx context.Context, migration *Migration) error
	GetExecutionPlan(migrations []*Migration) ([]string, error)
	
	// NoSQL-specific methods
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error
	GetDatabaseInfo(ctx context.Context) (*NoSQLDatabaseInfo, error)
}

// NoSQLMigrationTracker defines the interface for tracking NoSQL migration state
type NoSQLMigrationTracker interface {
	GetExecutedMigrations(ctx context.Context) ([]MigrationRecord, error)
	GetFailedMigrations(ctx context.Context) ([]MigrationRecord, error)
	RecordMigrationSuccess(ctx context.Context, migration MigrationRecord, executionTime int64) error
	RecordMigrationFailure(ctx context.Context, migration MigrationRecord, errorMsg string) error
	RemoveMigration(ctx context.Context, migrationID string) error
	IsLocked(ctx context.Context) (bool, error)
	AcquireLock(ctx context.Context) error
	ReleaseLock(ctx context.Context) error
	EnsureSchemaCollection(ctx context.Context) error
	GetCurrentVersion(ctx context.Context) (string, error)
	IsMigrationApplied(ctx context.Context, id string) (bool, error)
	GetMigration(ctx context.Context, id string) (*Migration, error)
	GetMigrationByVersion(ctx context.Context, version string) (*Migration, error)
	GetAppliedMigrations(ctx context.Context) ([]*Migration, error)
	
	// NoSQL-specific methods
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetCollectionInfo(ctx context.Context) (*NoSQLCollectionInfo, error)
}

// NoSQLMigrationValidator defines the interface for validating NoSQL migrations
type NoSQLMigrationValidator interface {
	ValidateMigrations(migrations []Migration) error
	ValidateMigration(migration *Migration) error
	CheckDependencies(migrations []Migration) error
	CheckCircularDependencies(migrations []Migration) error
	
	// NoSQL-specific validation methods
	ValidateSchema(ctx context.Context, schema interface{}) error
	ValidateIndexes(ctx context.Context, indexes []NoSQLIndex) error
	ValidateQuery(ctx context.Context, query string) error
	ValidateConnection(ctx context.Context) error
}

// NoSQLLockManager defines the interface for NoSQL-specific lock management
type NoSQLLockManager interface {
	AcquireLock(ctx context.Context, lockID string, timeout time.Duration) error
	ReleaseLock(ctx context.Context, lockID string) error
	RefreshLock(ctx context.Context, lockID string) error
	IsLocked(ctx context.Context, lockID string) (bool, error)
	EnsureLockCollection(ctx context.Context) error
	
	// NoSQL-specific methods
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	GetLockInfo(ctx context.Context, lockID string) (*NoSQLLockInfo, error)
}

// NoSQLDatabaseInfo contains information about the NoSQL database
type NoSQLDatabaseInfo struct {
	Type           string            `json:"type"`           // cassandra, mongodb, redis
	Version        string            `json:"version"`
	Host           string            `json:"host"`
	Port           int               `json:"port"`
	Database       string            `json:"database"`
	Keyspace       string            `json:"keyspace,omitempty"`       // Cassandra
	ReplicaSet     string            `json:"replica_set,omitempty"`    // MongoDB
	ClusterMode    bool              `json:"cluster_mode,omitempty"`   // Redis
	Nodes          []string          `json:"nodes,omitempty"`          // Cluster nodes
	Features       map[string]bool   `json:"features"`
	Configuration  map[string]string `json:"configuration"`
	Status         string            `json:"status"`
	LastChecked    time.Time         `json:"last_checked"`
}

// NoSQLCollectionInfo contains information about NoSQL collections/tables
type NoSQLCollectionInfo struct {
	Name           string                 `json:"name"`
	Type           string                 `json:"type"`           // collection, table, key
	Schema         map[string]interface{} `json:"schema,omitempty"`
	Indexes        []NoSQLIndex           `json:"indexes,omitempty"`
	Size           int64                  `json:"size"`
	DocumentCount  int64                  `json:"document_count,omitempty"`
	Partitions     []string               `json:"partitions,omitempty"`     // Cassandra
	ShardKey       map[string]interface{} `json:"shard_key,omitempty"`      // MongoDB
	TTL            *time.Duration         `json:"ttl,omitempty"`            // Redis
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// NoSQLIndex represents an index in NoSQL databases
type NoSQLIndex struct {
	Name       string                 `json:"name"`
	Fields     map[string]interface{} `json:"fields"`
	Type       string                 `json:"type"`       // btree, hash, text, geo, etc.
	Unique     bool                   `json:"unique"`
	Sparse     bool                   `json:"sparse,omitempty"`
	Background bool                   `json:"background,omitempty"`
	TTL        *time.Duration         `json:"ttl,omitempty"`
	Options    map[string]interface{} `json:"options,omitempty"`
}

// NoSQLLockInfo contains information about NoSQL locks
type NoSQLLockInfo struct {
	ID          string                 `json:"id"`
	AcquiredBy  string                 `json:"acquired_by"`
	AcquiredAt  time.Time              `json:"acquired_at"`
	ExpiresAt   time.Time              `json:"expires_at"`
	Hostname    string                 `json:"hostname"`
	ProcessID   int                    `json:"process_id"`
	Operation   string                 `json:"operation"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	Status      string                 `json:"status"`
}

// NoSQLMigrationScript represents a NoSQL migration script
type NoSQLMigrationScript struct {
	Type        string                 `json:"type"`        // javascript, cql, lua, etc.
	Content     string                 `json:"content"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Options     map[string]interface{} `json:"options,omitempty"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
	Retries     int                    `json:"retries,omitempty"`
	RetryDelay  time.Duration          `json:"retry_delay,omitempty"`
}

// NoSQLMigrationResult represents the result of a NoSQL migration
type NoSQLMigrationResult struct {
	Success       bool                   `json:"success"`
	ExecutionTime time.Duration          `json:"execution_time"`
	AffectedItems int64                  `json:"affected_items"`
	Output        string                 `json:"output,omitempty"`
	Warnings      []string               `json:"warnings,omitempty"`
	Errors        []string               `json:"errors,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// NoSQLConnectionConfig represents connection configuration for NoSQL databases
type NoSQLConnectionConfig struct {
	Type                string            `json:"type"`                  // cassandra, mongodb, redis
	Hosts               []string          `json:"hosts"`
	Port                int               `json:"port"`
	Database            string            `json:"database,omitempty"`
	Keyspace            string            `json:"keyspace,omitempty"`    // Cassandra
	Username            string            `json:"username,omitempty"`
	Password            string            `json:"password,omitempty"`
	AuthDatabase        string            `json:"auth_database,omitempty"` // MongoDB
	ReplicaSet          string            `json:"replica_set,omitempty"`   // MongoDB
	SSL                 bool              `json:"ssl"`
	SSLCertPath         string            `json:"ssl_cert_path,omitempty"`
	SSLKeyPath          string            `json:"ssl_key_path,omitempty"`
	SSLCAPath           string            `json:"ssl_ca_path,omitempty"`
	ConnectTimeout      time.Duration     `json:"connect_timeout"`
	SocketTimeout       time.Duration     `json:"socket_timeout"`
	MaxConnections      int               `json:"max_connections"`
	MinConnections      int               `json:"min_connections"`
	MaxIdleTime         time.Duration     `json:"max_idle_time"`
	HeartbeatInterval   time.Duration     `json:"heartbeat_interval"`
	Options             map[string]string `json:"options,omitempty"`
}
