/**
 * @file registry.go
 * @description Backend registry implementation for managing database-specific backends
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/registry.go
 */

package backend

import (
	"fmt"
	"sync"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DefaultBackendRegistry implements BackendRegistry
type DefaultBackendRegistry struct {
	mu        sync.RWMutex
	factories map[DatabaseDialect]BackendFactory
	dialects  map[DatabaseDialect]DialectInfo
}

// NewBackendRegistry creates a new backend registry
func NewBackendRegistry() BackendRegistry {
	registry := &DefaultBackendRegistry{
		factories: make(map[DatabaseDialect]BackendFactory),
		dialects:  make(map[DatabaseDialect]DialectInfo),
	}
	
	// Register default dialect information
	registry.registerDialectInfo()
	
	return registry
}

// Register registers a backend factory for a specific dialect
func (r *DefaultBackendRegistry) Register(dialect DatabaseDialect, factory BackendFactory) error {
	if dialect == "" {
		return fmt.Errorf("dialect cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[dialect] = factory
	return nil
}

// Unregister removes a backend factory for a specific dialect
func (r *DefaultBackendRegistry) Unregister(dialect DatabaseDialect) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[dialect]; !exists {
		return fmt.Errorf("dialect %s is not registered", dialect)
	}

	delete(r.factories, dialect)
	return nil
}

// Get retrieves a backend factory for a specific dialect
func (r *DefaultBackendRegistry) Get(dialect DatabaseDialect) (BackendFactory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	factory, exists := r.factories[dialect]
	if !exists {
		return nil, fmt.Errorf("no factory registered for dialect %s", dialect)
	}

	return factory, nil
}

// List returns all registered dialects
func (r *DefaultBackendRegistry) List() []DatabaseDialect {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dialects := make([]DatabaseDialect, 0, len(r.factories))
	for dialect := range r.factories {
		dialects = append(dialects, dialect)
	}

	return dialects
}

// IsSupported checks if a dialect is supported
func (r *DefaultBackendRegistry) IsSupported(dialect DatabaseDialect) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.factories[dialect]
	return exists
}

// GetDialectInfo returns information about a dialect
func (r *DefaultBackendRegistry) GetDialectInfo(dialect DatabaseDialect) (DialectInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.dialects[dialect]
	if !exists {
		return DialectInfo{}, fmt.Errorf("no information available for dialect %s", dialect)
	}

	return info, nil
}

// registerDialectInfo registers default dialect information
func (r *DefaultBackendRegistry) registerDialectInfo() {
	r.dialects[DialectPostgreSQL] = DialectInfo{
		Name:        "PostgreSQL",
		Version:     "9.6+",
		Features:    []string{"ACID", "JSON", "Arrays", "Full-text search", "Partitioning", "Replication"},
		Limitations: []string{"Case-sensitive identifiers", "Limited window functions in older versions"},
		DefaultPort: 5432,
		URLSchemes:  []string{"postgres", "postgresql"},
		FileExtensions: []string{".sql", ".psql"},
	}

	r.dialects[DialectMySQL] = DialectInfo{
		Name:        "MySQL",
		Version:     "5.7+",
		Features:    []string{"ACID", "JSON", "Full-text search", "Partitioning", "Replication"},
		Limitations: []string{"Limited window functions", "Case-insensitive by default", "Limited CTE support"},
		DefaultPort: 3306,
		URLSchemes:  []string{"mysql"},
		FileExtensions: []string{".sql", ".mysql"},
	}

	r.dialects[DialectSQLite] = DialectInfo{
		Name:        "SQLite",
		Version:     "3.8+",
		Features:    []string{"ACID", "JSON", "Full-text search", "Common Table Expressions"},
		Limitations: []string{"No ALTER COLUMN", "Limited concurrent writes", "No stored procedures"},
		DefaultPort: 0, // File-based
		URLSchemes:  []string{"sqlite", "sqlite3"},
		FileExtensions: []string{".db", ".sqlite", ".sqlite3"},
	}

	r.dialects[DialectSQLServer] = DialectInfo{
		Name:        "SQL Server",
		Version:     "2012+",
		Features:    []string{"ACID", "JSON", "XML", "Full-text search", "Partitioning", "Replication"},
		Limitations: []string{"Windows-centric", "Expensive licensing", "Limited cross-platform support"},
		DefaultPort: 1433,
		URLSchemes:  []string{"sqlserver", "mssql"},
		FileExtensions: []string{".sql", ".tsql"},
	}

	r.dialects[DialectOracle] = DialectInfo{
		Name:        "Oracle Database",
		Version:     "11g+",
		Features:    []string{"ACID", "JSON", "XML", "Advanced analytics", "Partitioning", "RAC"},
		Limitations: []string{"Expensive licensing", "Complex administration", "Proprietary features"},
		DefaultPort: 1521,
		URLSchemes:  []string{"oracle", "oci"},
		FileExtensions: []string{".sql", ".plsql"},
	}

	r.dialects[DialectDB2] = DialectInfo{
		Name:        "IBM DB2",
		Version:     "10.5+",
		Features:    []string{"ACID", "JSON", "XML", "Advanced analytics", "Partitioning", "Replication"},
		Limitations: []string{"Expensive licensing", "Complex administration", "Limited community support"},
		DefaultPort: 50000,
		URLSchemes:  []string{"db2"},
		FileExtensions: []string{".sql", ".db2"},
	}

	r.dialects[DialectCassandra] = DialectInfo{
		Name:        "Apache Cassandra",
		Version:     "3.0+",
		Features:    []string{"NoSQL", "Distributed", "High availability", "Linear scalability"},
		Limitations: []string{"No ACID transactions", "Limited query capabilities", "Eventual consistency"},
		DefaultPort: 9042,
		URLSchemes:  []string{"cassandra", "cql"},
		FileExtensions: []string{".cql"},
	}

	r.dialects[DialectMongoDB] = DialectInfo{
		Name:        "MongoDB",
		Version:     "4.0+",
		Features:    []string{"Document store", "JSON", "Aggregation", "Sharding", "Replication"},
		Limitations: []string{"No ACID transactions across documents", "Memory usage", "Complex queries"},
		DefaultPort: 27017,
		URLSchemes:  []string{"mongodb", "mongodb+srv"},
		FileExtensions: []string{".js", ".json"},
	}

	r.dialects[DialectRedis] = DialectInfo{
		Name:        "Redis",
		Version:     "5.0+",
		Features:    []string{"In-memory", "Key-value store", "Pub/Sub", "Lua scripting", "Clustering"},
		Limitations: []string{"Memory-bound", "Limited query capabilities", "No complex data types"},
		DefaultPort: 6379,
		URLSchemes:  []string{"redis", "rediss"},
		FileExtensions: []string{".redis", ".lua"},
	}
}

// DefaultBackendFactory implements BackendFactory
type DefaultBackendFactory struct {
	dialect DatabaseDialect
	creator func(config types.DatabaseConfig) (DatabaseBackend, error)
}

// NewBackendFactory creates a new backend factory
func NewBackendFactory(dialect DatabaseDialect, creator func(config types.DatabaseConfig) (DatabaseBackend, error)) BackendFactory {
	return &DefaultBackendFactory{
		dialect: dialect,
		creator: creator,
	}
}

// CreateBackend creates a backend instance
func (f *DefaultBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != f.dialect {
		return nil, fmt.Errorf("factory does not support dialect %s", dialect)
	}

	return f.creator(config)
}

// SupportedDialects returns supported dialects
func (f *DefaultBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{f.dialect}
}

// GetDialectInfo returns dialect information
func (f *DefaultBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	registry := NewBackendRegistry()
	info, _ := registry.GetDialectInfo(dialect)
	return info
}

// GlobalBackendRegistry is the global registry instance
var GlobalBackendRegistry = NewBackendRegistry()

// RegisterBackend registers a backend factory globally
func RegisterBackend(dialect DatabaseDialect, factory BackendFactory) error {
	return GlobalBackendRegistry.Register(dialect, factory)
}

// GetBackend retrieves a backend factory globally
func GetBackend(dialect DatabaseDialect) (BackendFactory, error) {
	return GlobalBackendRegistry.Get(dialect)
}

// CreateBackend creates a backend instance using the global registry
func CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	factory, err := GetBackend(dialect)
	if err != nil {
		return nil, err
	}

	return factory.CreateBackend(dialect, config)
}

// IsDialectSupported checks if a dialect is supported globally
func IsDialectSupported(dialect DatabaseDialect) bool {
	return GlobalBackendRegistry.IsSupported(dialect)
}

// ListSupportedDialects returns all supported dialects globally
func ListSupportedDialects() []DatabaseDialect {
	return GlobalBackendRegistry.List()
}

// GetDialectInfo returns dialect information globally
func GetDialectInfo(dialect DatabaseDialect) (DialectInfo, error) {
	return GlobalBackendRegistry.GetDialectInfo(dialect)
}

// BackendBuilder helps build backend configurations
type BackendBuilder struct {
	dialect DatabaseDialect
	config  types.DatabaseConfig
}

// NewBackendBuilder creates a new backend builder
func NewBackendBuilder(dialect DatabaseDialect) *BackendBuilder {
	return &BackendBuilder{
		dialect: dialect,
		config:  types.DatabaseConfig{},
	}
}

// WithHost sets the database host
func (b *BackendBuilder) WithHost(host string) *BackendBuilder {
	b.config.Host = host
	return b
}

// WithPort sets the database port
func (b *BackendBuilder) WithPort(port int) *BackendBuilder {
	b.config.Port = port
	return b
}

// WithDatabase sets the database name
func (b *BackendBuilder) WithDatabase(database string) *BackendBuilder {
	b.config.Database = database
	return b
}

// WithUsername sets the username
func (b *BackendBuilder) WithUsername(username string) *BackendBuilder {
	b.config.Username = username
	return b
}

// WithPassword sets the password
func (b *BackendBuilder) WithPassword(password string) *BackendBuilder {
	b.config.Password = password
	return b
}

// WithSSL enables SSL
func (b *BackendBuilder) WithSSL(ssl bool) *BackendBuilder {
	if ssl {
		b.config.SSLMode = "require"
	} else {
		b.config.SSLMode = "disable"
	}
	return b
}

// WithTimeout sets the connection timeout (in seconds)
func (b *BackendBuilder) WithTimeout(timeout int) *BackendBuilder {
	b.config.ConnectionTimeout = time.Duration(timeout) * time.Second
	return b
}

// WithMaxConnections sets the maximum connections
func (b *BackendBuilder) WithMaxConnections(maxConnections int) *BackendBuilder {
	b.config.MaxConnections = maxConnections
	return b
}

// WithOptions sets additional options
func (b *BackendBuilder) WithOptions(options map[string]interface{}) *BackendBuilder {
	// Convert map[string]interface{} to map[string]string
	stringOptions := make(map[string]string)
	for k, v := range options {
		if str, ok := v.(string); ok {
			stringOptions[k] = str
		} else {
			stringOptions[k] = fmt.Sprintf("%v", v)
		}
	}
	b.config.Options = stringOptions
	return b
}

// Build creates the backend instance
func (b *BackendBuilder) Build() (DatabaseBackend, error) {
	return CreateBackend(b.dialect, b.config)
}

// GetConfig returns the current configuration
func (b *BackendBuilder) GetConfig() types.DatabaseConfig {
	return b.config
}

// GetDialect returns the dialect
func (b *BackendBuilder) GetDialect() DatabaseDialect {
	return b.dialect
}
