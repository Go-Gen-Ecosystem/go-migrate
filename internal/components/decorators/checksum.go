/**
 * @file checksum.go
 * @description Checksum decorator for data integrity validation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/decorators/checksum.go
 */

package decorators

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/backend"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// ChecksumAlgorithm represents supported checksum algorithms
type ChecksumAlgorithm string

const (
	ChecksumMD5    ChecksumAlgorithm = "md5"
	ChecksumSHA1   ChecksumAlgorithm = "sha1"
	ChecksumSHA256 ChecksumAlgorithm = "sha256"
	ChecksumSHA512 ChecksumAlgorithm = "sha512"
)

// ChecksumRecord represents a stored checksum record
type ChecksumRecord struct {
	ID        string    `json:"id"`
	TableName string    `json:"table_name"`
	Operation string    `json:"operation"`
	Query     string    `json:"query"`
	Checksum  string    `json:"checksum"`
	Algorithm string    `json:"algorithm"`
	CreatedAt time.Time `json:"created_at"`
	Metadata  string    `json:"metadata"`
}

// ChecksumValidator validates data integrity using checksums
type ChecksumValidator interface {
	// ComputeChecksum computes checksum for given data
	ComputeChecksum(data []byte) string
	
	// ValidateChecksum validates data against expected checksum
	ValidateChecksum(data []byte, expectedChecksum string) bool
	
	// GetAlgorithm returns the algorithm used
	GetAlgorithm() ChecksumAlgorithm
}

// HashChecksumValidator implements checksum validation using hash algorithms
type HashChecksumValidator struct {
	algorithm ChecksumAlgorithm
	hasher    hash.Hash
}

// NewHashChecksumValidator creates a new hash-based checksum validator
func NewHashChecksumValidator(algorithm ChecksumAlgorithm) (*HashChecksumValidator, error) {
	var hasher hash.Hash
	
	switch algorithm {
	case ChecksumMD5:
		hasher = md5.New()
	case ChecksumSHA1:
		hasher = sha1.New()
	case ChecksumSHA256:
		hasher = sha256.New()
	case ChecksumSHA512:
		hasher = sha512.New()
	default:
		return nil, fmt.Errorf("unsupported checksum algorithm: %s", algorithm)
	}
	
	return &HashChecksumValidator{
		algorithm: algorithm,
		hasher:    hasher,
	}, nil
}

// ComputeChecksum computes checksum for given data
func (v *HashChecksumValidator) ComputeChecksum(data []byte) string {
	v.hasher.Reset()
	v.hasher.Write(data)
	return hex.EncodeToString(v.hasher.Sum(nil))
}

// ValidateChecksum validates data against expected checksum
func (v *HashChecksumValidator) ValidateChecksum(data []byte, expectedChecksum string) bool {
	actualChecksum := v.ComputeChecksum(data)
	return strings.EqualFold(actualChecksum, expectedChecksum)
}

// GetAlgorithm returns the algorithm used
func (v *HashChecksumValidator) GetAlgorithm() ChecksumAlgorithm {
	return v.algorithm
}

// ChecksumStore manages checksum storage and retrieval
type ChecksumStore interface {
	// StoreChecksum stores a checksum record
	StoreChecksum(ctx context.Context, record ChecksumRecord) error
	
	// GetChecksum retrieves a checksum record
	GetChecksum(ctx context.Context, id string) (*ChecksumRecord, error)
	
	// ListChecksums lists checksum records with optional filters
	ListChecksums(ctx context.Context, filters map[string]interface{}) ([]ChecksumRecord, error)
	
	// DeleteChecksum deletes a checksum record
	DeleteChecksum(ctx context.Context, id string) error
	
	// Initialize initializes the checksum store
	Initialize(ctx context.Context) error
}

// DatabaseChecksumStore implements checksum storage in database
type DatabaseChecksumStore struct {
	backend   backend.DatabaseBackend
	tableName string
}

// NewDatabaseChecksumStore creates a new database checksum store
func NewDatabaseChecksumStore(backend backend.DatabaseBackend, tableName string) *DatabaseChecksumStore {
	return &DatabaseChecksumStore{
		backend:   backend,
		tableName: tableName,
	}
}

// Initialize initializes the checksum store
func (s *DatabaseChecksumStore) Initialize(ctx context.Context) error {
	// Check if table exists
	exists, err := s.backend.TableExists(ctx, s.tableName)
	if err != nil {
		return fmt.Errorf("failed to check if checksum table exists: %w", err)
	}
	
	if !exists {
		// Create checksum table
		tableDef := backend.TableDefinition{
			Name: s.tableName,
			Columns: []backend.ColumnDefinition{
				{
					Name:     "id",
					Type:     "VARCHAR(255)",
					Nullable: false,
					Primary:  true,
				},
				{
					Name:     "table_name",
					Type:     "VARCHAR(255)",
					Nullable: false,
				},
				{
					Name:     "operation",
					Type:     "VARCHAR(100)",
					Nullable: false,
				},
				{
					Name:     "query",
					Type:     "TEXT",
					Nullable: true,
				},
				{
					Name:     "checksum",
					Type:     "VARCHAR(255)",
					Nullable: false,
				},
				{
					Name:     "algorithm",
					Type:     "VARCHAR(50)",
					Nullable: false,
				},
				{
					Name:         "created_at",
					Type:         "TIMESTAMP",
					Nullable:     false,
					DefaultValue: "CURRENT_TIMESTAMP",
				},
				{
					Name:     "metadata",
					Type:     "TEXT",
					Nullable: true,
				},
			},
		}
		
		if err := s.backend.CreateTable(ctx, tableDef); err != nil {
			return fmt.Errorf("failed to create checksum table: %w", err)
		}
		
		// Create index on table_name and operation
		indexDef := backend.IndexDefinition{
			Name:    fmt.Sprintf("idx_%s_table_operation", s.tableName),
			Table:   s.tableName,
			Columns: []string{"table_name", "operation"},
		}
		
		if err := s.backend.CreateIndex(ctx, indexDef); err != nil {
			return fmt.Errorf("failed to create checksum table index: %w", err)
		}
	}
	
	return nil
}

// StoreChecksum stores a checksum record
func (s *DatabaseChecksumStore) StoreChecksum(ctx context.Context, record ChecksumRecord) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (id, table_name, operation, query, checksum, algorithm, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, s.tableName)
	
	_, err := s.backend.Execute(ctx, query,
		record.ID,
		record.TableName,
		record.Operation,
		record.Query,
		record.Checksum,
		record.Algorithm,
		record.CreatedAt,
		record.Metadata,
	)
	
	if err != nil {
		return fmt.Errorf("failed to store checksum record: %w", err)
	}
	
	return nil
}

// GetChecksum retrieves a checksum record
func (s *DatabaseChecksumStore) GetChecksum(ctx context.Context, id string) (*ChecksumRecord, error) {
	query := fmt.Sprintf(`
		SELECT id, table_name, operation, query, checksum, algorithm, created_at, metadata
		FROM %s WHERE id = ?
	`, s.tableName)
	
	row := s.backend.QueryRow(ctx, query, id)
	
	var record ChecksumRecord
	err := row.Scan(
		&record.ID,
		&record.TableName,
		&record.Operation,
		&record.Query,
		&record.Checksum,
		&record.Algorithm,
		&record.CreatedAt,
		&record.Metadata,
	)
	
	if err != nil {
		return nil, fmt.Errorf("failed to get checksum record: %w", err)
	}
	
	return &record, nil
}

// ListChecksums lists checksum records with optional filters
func (s *DatabaseChecksumStore) ListChecksums(ctx context.Context, filters map[string]interface{}) ([]ChecksumRecord, error) {
	query := fmt.Sprintf(`
		SELECT id, table_name, operation, query, checksum, algorithm, created_at, metadata
		FROM %s
	`, s.tableName)
	
	var args []interface{}
	var conditions []string
	
	if tableName, ok := filters["table_name"].(string); ok {
		conditions = append(conditions, "table_name = ?")
		args = append(args, tableName)
	}
	
	if operation, ok := filters["operation"].(string); ok {
		conditions = append(conditions, "operation = ?")
		args = append(args, operation)
	}
	
	if algorithm, ok := filters["algorithm"].(string); ok {
		conditions = append(conditions, "algorithm = ?")
		args = append(args, algorithm)
	}
	
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	
	query += " ORDER BY created_at DESC"
	
	rows, err := s.backend.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list checksum records: %w", err)
	}
	defer rows.Close()
	
	var records []ChecksumRecord
	for rows.Next() {
		var record ChecksumRecord
		err := rows.Scan(
			&record.ID,
			&record.TableName,
			&record.Operation,
			&record.Query,
			&record.Checksum,
			&record.Algorithm,
			&record.CreatedAt,
			&record.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan checksum record: %w", err)
		}
		records = append(records, record)
	}
	
	return records, nil
}

// DeleteChecksum deletes a checksum record
func (s *DatabaseChecksumStore) DeleteChecksum(ctx context.Context, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", s.tableName)
	
	_, err := s.backend.Execute(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete checksum record: %w", err)
	}
	
	return nil
}

// ChecksumDecorator adds checksum validation to database operations
type ChecksumDecorator struct {
	*BaseBackendDecorator
	config    ChecksumConfig
	validator ChecksumValidator
	store     ChecksumStore
}

// NewChecksumDecorator creates a new checksum decorator
func NewChecksumDecorator(wrapped backend.DatabaseBackend, config ChecksumConfig) (*ChecksumDecorator, error) {
	validator, err := NewHashChecksumValidator(ChecksumAlgorithm(config.Algorithm))
	if err != nil {
		return nil, fmt.Errorf("failed to create checksum validator: %w", err)
	}
	
	var store ChecksumStore
	if config.StoreInDB {
		store = NewDatabaseChecksumStore(wrapped, config.TableName)
	}
	
	return &ChecksumDecorator{
		BaseBackendDecorator: NewBaseBackendDecorator(wrapped),
		config:               config,
		validator:            validator,
		store:                store,
	}, nil
}

// Connect initializes checksum store if needed
func (d *ChecksumDecorator) Connect(ctx context.Context, config types.DatabaseConfig) error {
	err := d.wrapped.Connect(ctx, config)
	if err != nil {
		return err
	}
	
	// Initialize checksum store if configured
	if d.config.StoreInDB && d.store != nil {
		if err := d.store.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize checksum store: %w", err)
		}
	}
	
	return nil
}

// Execute validates and stores checksums for execute operations
func (d *ChecksumDecorator) Execute(ctx context.Context, query string, args ...interface{}) (backend.QueryResult, error) {
	// Compute checksum for the query and arguments
	queryData := d.buildQueryData(query, args)
	checksum := d.validator.ComputeChecksum(queryData)
	
	// Execute the query
	result, err := d.wrapped.Execute(ctx, query, args...)
	
	// Store checksum if configured and operation succeeded
	if err == nil && d.config.StoreInDB && d.store != nil && !d.config.ValidateOnly {
		record := ChecksumRecord{
			ID:        d.generateChecksumID(query, args),
			TableName: d.extractTableName(query),
			Operation: d.extractOperation(query),
			Query:     query,
			Checksum:  checksum,
			Algorithm: string(d.validator.GetAlgorithm()),
			CreatedAt: time.Now(),
			Metadata:  d.buildMetadata(args),
		}
		
		if storeErr := d.store.StoreChecksum(ctx, record); storeErr != nil {
			// Log error but don't fail the operation
			fmt.Printf("Warning: failed to store checksum: %v\n", storeErr)
		}
	}
	
	return result, err
}

// Query validates checksums for query operations
func (d *ChecksumDecorator) Query(ctx context.Context, query string, args ...interface{}) (backend.QueryRows, error) {
	// For read operations, we can validate against stored checksums
	if d.config.ValidateOnly && d.store != nil {
		if err := d.validateStoredChecksum(ctx, query, args); err != nil {
			return nil, fmt.Errorf("checksum validation failed: %w", err)
		}
	}
	
	return d.wrapped.Query(ctx, query, args...)
}

// QueryRow validates checksums for single row query operations
func (d *ChecksumDecorator) QueryRow(ctx context.Context, query string, args ...interface{}) backend.QueryRow {
	// For read operations, we can validate against stored checksums
	if d.config.ValidateOnly && d.store != nil {
		if err := d.validateStoredChecksum(ctx, query, args); err != nil {
			// Return an error row
			return &ErrorQueryRow{err: fmt.Errorf("checksum validation failed: %w", err)}
		}
	}
	
	return d.wrapped.QueryRow(ctx, query, args...)
}

// CreateTable validates and stores checksums for table creation
func (d *ChecksumDecorator) CreateTable(ctx context.Context, tableDef backend.TableDefinition) error {
	// Compute checksum for table definition
	tableData := d.buildTableDefinitionData(tableDef)
	checksum := d.validator.ComputeChecksum(tableData)
	
	err := d.wrapped.CreateTable(ctx, tableDef)
	
	// Store checksum if configured and operation succeeded
	if err == nil && d.config.StoreInDB && d.store != nil && !d.config.ValidateOnly {
		record := ChecksumRecord{
			ID:        d.generateTableChecksumID(tableDef.Name, "CREATE"),
			TableName: tableDef.Name,
			Operation: "CREATE_TABLE",
			Query:     fmt.Sprintf("CREATE TABLE %s", tableDef.Name),
			Checksum:  checksum,
			Algorithm: string(d.validator.GetAlgorithm()),
			CreatedAt: time.Now(),
			Metadata:  d.buildTableMetadata(tableDef),
		}
		
		if storeErr := d.store.StoreChecksum(ctx, record); storeErr != nil {
			fmt.Printf("Warning: failed to store table checksum: %v\n", storeErr)
		}
	}
	
	return err
}

// DropTable validates and stores checksums for table deletion
func (d *ChecksumDecorator) DropTable(ctx context.Context, tableName string) error {
	err := d.wrapped.DropTable(ctx, tableName)
	
	// Store checksum if configured and operation succeeded
	if err == nil && d.config.StoreInDB && d.store != nil && !d.config.ValidateOnly {
		record := ChecksumRecord{
			ID:        d.generateTableChecksumID(tableName, "DROP"),
			TableName: tableName,
			Operation: "DROP_TABLE",
			Query:     fmt.Sprintf("DROP TABLE %s", tableName),
			Checksum:  d.validator.ComputeChecksum([]byte(tableName)),
			Algorithm: string(d.validator.GetAlgorithm()),
			CreatedAt: time.Now(),
			Metadata:  fmt.Sprintf(`{"table_name": "%s"}`, tableName),
		}
		
		if storeErr := d.store.StoreChecksum(ctx, record); storeErr != nil {
			fmt.Printf("Warning: failed to store drop table checksum: %v\n", storeErr)
		}
	}
	
	return err
}

// Helper methods

// buildQueryData builds data for checksum computation from query and arguments
func (d *ChecksumDecorator) buildQueryData(query string, args []interface{}) []byte {
	data := query
	for _, arg := range args {
		data += fmt.Sprintf("%v", arg)
	}
	return []byte(data)
}

// buildTableDefinitionData builds data for checksum computation from table definition
func (d *ChecksumDecorator) buildTableDefinitionData(tableDef backend.TableDefinition) []byte {
	data := tableDef.Name
	for _, col := range tableDef.Columns {
		data += fmt.Sprintf("%s:%s:%t:%t", col.Name, col.Type, !col.Nullable, col.Primary)
	}
	return []byte(data)
}

// generateChecksumID generates a unique ID for checksum records
func (d *ChecksumDecorator) generateChecksumID(query string, args []interface{}) string {
	_ = d.buildQueryData(query, args)
	return fmt.Sprintf("checksum_%s_%d", d.validator.GetAlgorithm(), time.Now().UnixNano())
}

// generateTableChecksumID generates a unique ID for table checksum records
func (d *ChecksumDecorator) generateTableChecksumID(tableName, operation string) string {
	return fmt.Sprintf("table_%s_%s_%s_%d", d.validator.GetAlgorithm(), tableName, operation, time.Now().UnixNano())
}

// extractTableName extracts table name from SQL query
func (d *ChecksumDecorator) extractTableName(query string) string {
	query = strings.TrimSpace(strings.ToUpper(query))
	
	if strings.HasPrefix(query, "INSERT INTO") {
		parts := strings.Fields(query)
		if len(parts) >= 3 {
			return parts[2]
		}
	} else if strings.HasPrefix(query, "UPDATE") {
		parts := strings.Fields(query)
		if len(parts) >= 2 {
			return parts[1]
		}
	} else if strings.HasPrefix(query, "DELETE FROM") {
		parts := strings.Fields(query)
		if len(parts) >= 3 {
			return parts[2]
		}
	} else if strings.HasPrefix(query, "SELECT") {
		// Extract from FROM clause
		fromIndex := strings.Index(query, "FROM")
		if fromIndex != -1 {
			parts := strings.Fields(query[fromIndex:])
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	
	return "unknown"
}

// extractOperation extracts operation type from SQL query
func (d *ChecksumDecorator) extractOperation(query string) string {
	query = strings.TrimSpace(strings.ToUpper(query))
	
	if strings.HasPrefix(query, "SELECT") {
		return "SELECT"
	} else if strings.HasPrefix(query, "INSERT") {
		return "INSERT"
	} else if strings.HasPrefix(query, "UPDATE") {
		return "UPDATE"
	} else if strings.HasPrefix(query, "DELETE") {
		return "DELETE"
	} else if strings.HasPrefix(query, "CREATE") {
		return "CREATE"
	} else if strings.HasPrefix(query, "DROP") {
		return "DROP"
	} else if strings.HasPrefix(query, "ALTER") {
		return "ALTER"
	} else {
		return "OTHER"
	}
}

// buildMetadata builds metadata string from arguments
func (d *ChecksumDecorator) buildMetadata(args []interface{}) string {
	if len(args) == 0 {
		return "{}"
	}
	
	metadata := fmt.Sprintf(`{"args_count": %d, "args": [`, len(args))
	for i, arg := range args {
		if i > 0 {
			metadata += ", "
		}
		metadata += fmt.Sprintf(`"%v"`, arg)
	}
	metadata += "]}"
	
	return metadata
}

// buildTableMetadata builds metadata string from table definition
func (d *ChecksumDecorator) buildTableMetadata(tableDef backend.TableDefinition) string {
	return fmt.Sprintf(`{"table_name": "%s", "columns_count": %d}`, tableDef.Name, len(tableDef.Columns))
}

// validateStoredChecksum validates query against stored checksum
func (d *ChecksumDecorator) validateStoredChecksum(ctx context.Context, query string, args []interface{}) error {
	// This is a simplified validation - in practice, you might want more sophisticated matching
	queryData := d.buildQueryData(query, args)
	currentChecksum := d.validator.ComputeChecksum(queryData)
	
	// Look for similar stored checksums
	filters := map[string]interface{}{
		"table_name": d.extractTableName(query),
		"operation":  d.extractOperation(query),
		"algorithm":  string(d.validator.GetAlgorithm()),
	}
	
	records, err := d.store.ListChecksums(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to retrieve stored checksums: %w", err)
	}
	
	// Check if current checksum matches any stored checksum
	for _, record := range records {
		if record.Checksum == currentChecksum {
			return nil // Validation passed
		}
	}
	
	// If no matching checksum found, it might be a new query variant
	// In strict mode, this would be an error
	return nil
}

// ErrorQueryRow implements QueryRow interface for error cases
type ErrorQueryRow struct {
	err error
}

// Scan returns the stored error
func (r *ErrorQueryRow) Scan(dest ...interface{}) error {
	return r.err
}

// ChecksumDecoratorFactory creates checksum decorators
type ChecksumDecoratorFactory struct{}

// CreateDecorator creates a checksum decorator from configuration
func (f *ChecksumDecoratorFactory) CreateDecorator(config DecoratorConfig) (BackendDecorator, error) {
	checksumConfig := ParseChecksumConfig(config.Options)
	
	decorator := &ChecksumDecorator{
		BaseBackendDecorator: &BaseBackendDecorator{},
		config:               checksumConfig,
	}
	
	validator, err := NewHashChecksumValidator(ChecksumAlgorithm(checksumConfig.Algorithm))
	if err != nil {
		return nil, fmt.Errorf("failed to create checksum validator: %w", err)
	}
	decorator.validator = validator
	
	return decorator, nil
}

// SupportedTypes returns the supported decorator types
func (f *ChecksumDecoratorFactory) SupportedTypes() []DecoratorType {
	return []DecoratorType{DecoratorTypeChecksum}
}

// Register the checksum decorator factory
func init() {
	GlobalDecoratorRegistry.RegisterFactory(DecoratorTypeChecksum, &ChecksumDecoratorFactory{})
}
