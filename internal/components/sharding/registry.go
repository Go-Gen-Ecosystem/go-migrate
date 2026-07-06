/**
 * @file registry.go
 * @description Tenant registry implementation for managing tenant-to-shard mappings
 * @author Claude Code
 * @date 2025-10-31
 * @path pkg/core/database/migrations/internal/components/sharding/registry.go
 */

package sharding

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// DatabaseTenantRegistry implements TenantRegistry using a database backend
type DatabaseTenantRegistry struct {
	db              *sql.DB
	tableName       string
	cache           map[string]*Tenant
	cacheMu         sync.RWMutex
	cacheTTL        time.Duration
	cacheTimestamps map[string]time.Time
}

// NewDatabaseTenantRegistry creates a new database-backed tenant registry
func NewDatabaseTenantRegistry(db *sql.DB, tableName string) *DatabaseTenantRegistry {
	if tableName == "" {
		tableName = "tenant_registry"
	}

	return &DatabaseTenantRegistry{
		db:              db,
		tableName:       tableName,
		cache:           make(map[string]*Tenant),
		cacheTTL:        5 * time.Minute,
		cacheTimestamps: make(map[string]time.Time),
	}
}

// EnsureTable creates the tenant registry table if it doesn't exist
func (r *DatabaseTenantRegistry) EnsureTable(ctx context.Context) error {
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			shard_id VARCHAR(255),
			schema_name VARCHAR(255),
			database_name VARCHAR(255),
			status VARCHAR(50) NOT NULL DEFAULT 'active',
			metadata TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			migration_state VARCHAR(255),
			INDEX idx_shard_id (shard_id),
			INDEX idx_status (status)
		)
	`, r.tableName)

	_, err := r.db.ExecContext(ctx, createSQL)
	return err
}

// GetTenant retrieves tenant information
func (r *DatabaseTenantRegistry) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	// Check cache first
	r.cacheMu.RLock()
	if cached, ok := r.cache[tenantID]; ok {
		if time.Since(r.cacheTimestamps[tenantID]) < r.cacheTTL {
			r.cacheMu.RUnlock()
			// Return a copy to prevent external modifications
			tenantCopy := *cached
			return &tenantCopy, nil
		}
	}
	r.cacheMu.RUnlock()

	// Query database
	query := fmt.Sprintf(`
		SELECT id, name, shard_id, schema_name, database_name, status,
		       metadata, created_at, updated_at, migration_state
		FROM %s
		WHERE id = ?
	`, r.tableName)

	var tenant Tenant
	var metadata sql.NullString
	var shardID, schemaName, databaseName, migrationState sql.NullString

	err := r.db.QueryRowContext(ctx, query, tenantID).Scan(
		&tenant.ID,
		&tenant.Name,
		&shardID,
		&schemaName,
		&databaseName,
		&tenant.Status,
		&metadata,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
		&migrationState,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("tenant %s not found", tenantID)
		}
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	// Handle nullable fields
	if shardID.Valid {
		tenant.ShardID = shardID.String
	}
	if schemaName.Valid {
		tenant.SchemaName = schemaName.String
	}
	if databaseName.Valid {
		tenant.DatabaseName = databaseName.String
	}
	if migrationState.Valid {
		tenant.MigrationState = migrationState.String
	}

	// Initialize metadata map
	tenant.Metadata = make(map[string]string)

	// Cache the result
	r.cacheMu.Lock()
	r.cache[tenantID] = &tenant
	r.cacheTimestamps[tenantID] = time.Now()
	r.cacheMu.Unlock()

	return &tenant, nil
}

// ListTenants returns all tenants
func (r *DatabaseTenantRegistry) ListTenants(ctx context.Context) ([]*Tenant, error) {
	query := fmt.Sprintf(`
		SELECT id, name, shard_id, schema_name, database_name, status,
		       metadata, created_at, updated_at, migration_state
		FROM %s
		ORDER BY created_at DESC
	`, r.tableName)

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant

	for rows.Next() {
		var tenant Tenant
		var metadata sql.NullString
		var shardID, schemaName, databaseName, migrationState sql.NullString

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&shardID,
			&schemaName,
			&databaseName,
			&tenant.Status,
			&metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&migrationState,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		if shardID.Valid {
			tenant.ShardID = shardID.String
		}
		if schemaName.Valid {
			tenant.SchemaName = schemaName.String
		}
		if databaseName.Valid {
			tenant.DatabaseName = databaseName.String
		}
		if migrationState.Valid {
			tenant.MigrationState = migrationState.String
		}

		tenant.Metadata = make(map[string]string)

		tenants = append(tenants, &tenant)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tenants: %w", err)
	}

	return tenants, nil
}

// ListTenantsByShard returns tenants on a specific shard
func (r *DatabaseTenantRegistry) ListTenantsByShard(ctx context.Context, shardID string) ([]*Tenant, error) {
	query := fmt.Sprintf(`
		SELECT id, name, shard_id, schema_name, database_name, status,
		       metadata, created_at, updated_at, migration_state
		FROM %s
		WHERE shard_id = ?
		ORDER BY created_at DESC
	`, r.tableName)

	rows, err := r.db.QueryContext(ctx, query, shardID)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants by shard: %w", err)
	}
	defer rows.Close()

	var tenants []*Tenant

	for rows.Next() {
		var tenant Tenant
		var metadata sql.NullString
		var shardIDVal, schemaName, databaseName, migrationState sql.NullString

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&shardIDVal,
			&schemaName,
			&databaseName,
			&tenant.Status,
			&metadata,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
			&migrationState,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		if shardIDVal.Valid {
			tenant.ShardID = shardIDVal.String
		}
		if schemaName.Valid {
			tenant.SchemaName = schemaName.String
		}
		if databaseName.Valid {
			tenant.DatabaseName = databaseName.String
		}
		if migrationState.Valid {
			tenant.MigrationState = migrationState.String
		}

		tenant.Metadata = make(map[string]string)

		tenants = append(tenants, &tenant)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tenants: %w", err)
	}

	return tenants, nil
}

// RegisterTenant registers a new tenant
func (r *DatabaseTenantRegistry) RegisterTenant(ctx context.Context, tenant *Tenant) error {
	if tenant.ID == "" {
		return fmt.Errorf("tenant ID cannot be empty")
	}

	if tenant.Name == "" {
		return fmt.Errorf("tenant name cannot be empty")
	}

	// Set defaults
	if tenant.Status == "" {
		tenant.Status = "active"
	}

	tenant.CreatedAt = time.Now()
	tenant.UpdatedAt = time.Now()

	insertSQL := fmt.Sprintf(`
		INSERT INTO %s (id, name, shard_id, schema_name, database_name, status,
		                metadata, created_at, updated_at, migration_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.tableName)

	_, err := r.db.ExecContext(ctx, insertSQL,
		tenant.ID,
		tenant.Name,
		toNullString(tenant.ShardID),
		toNullString(tenant.SchemaName),
		toNullString(tenant.DatabaseName),
		tenant.Status,
		nil, // metadata (JSON)
		tenant.CreatedAt,
		tenant.UpdatedAt,
		toNullString(tenant.MigrationState),
	)

	if err != nil {
		return fmt.Errorf("failed to register tenant: %w", err)
	}

	// Invalidate cache
	r.invalidateCache(tenant.ID)

	return nil
}

// UpdateTenant updates tenant information
func (r *DatabaseTenantRegistry) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	tenant.UpdatedAt = time.Now()

	updateSQL := fmt.Sprintf(`
		UPDATE %s
		SET name = ?, shard_id = ?, schema_name = ?, database_name = ?,
		    status = ?, metadata = ?, updated_at = ?, migration_state = ?
		WHERE id = ?
	`, r.tableName)

	result, err := r.db.ExecContext(ctx, updateSQL,
		tenant.Name,
		toNullString(tenant.ShardID),
		toNullString(tenant.SchemaName),
		toNullString(tenant.DatabaseName),
		tenant.Status,
		nil, // metadata (JSON)
		tenant.UpdatedAt,
		toNullString(tenant.MigrationState),
		tenant.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("tenant %s not found", tenant.ID)
	}

	// Invalidate cache
	r.invalidateCache(tenant.ID)

	return nil
}

// MoveTenant moves a tenant to a different shard
func (r *DatabaseTenantRegistry) MoveTenant(ctx context.Context, tenantID string, newShardID string) error {
	updateSQL := fmt.Sprintf(`
		UPDATE %s
		SET shard_id = ?, updated_at = ?
		WHERE id = ?
	`, r.tableName)

	result, err := r.db.ExecContext(ctx, updateSQL, newShardID, time.Now(), tenantID)
	if err != nil {
		return fmt.Errorf("failed to move tenant: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	// Invalidate cache
	r.invalidateCache(tenantID)

	return nil
}

// DeleteTenant removes a tenant
func (r *DatabaseTenantRegistry) DeleteTenant(ctx context.Context, tenantID string) error {
	deleteSQL := fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, r.tableName)

	result, err := r.db.ExecContext(ctx, deleteSQL, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	// Invalidate cache
	r.invalidateCache(tenantID)

	return nil
}

// invalidateCache removes a tenant from cache
func (r *DatabaseTenantRegistry) invalidateCache(tenantID string) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	delete(r.cache, tenantID)
	delete(r.cacheTimestamps, tenantID)
}

// ClearCache clears the entire cache
func (r *DatabaseTenantRegistry) ClearCache() {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	r.cache = make(map[string]*Tenant)
	r.cacheTimestamps = make(map[string]time.Time)
}

// GetCacheStats returns cache statistics
func (r *DatabaseTenantRegistry) GetCacheStats() map[string]int {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	return map[string]int{
		"size": len(r.cache),
	}
}

// Helper function to convert string to sql.NullString
func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// InMemoryTenantRegistry implements TenantRegistry using in-memory storage
type InMemoryTenantRegistry struct {
	tenants map[string]*Tenant
	mu      sync.RWMutex
}

// NewInMemoryTenantRegistry creates a new in-memory tenant registry
func NewInMemoryTenantRegistry() *InMemoryTenantRegistry {
	return &InMemoryTenantRegistry{
		tenants: make(map[string]*Tenant),
	}
}

// GetTenant retrieves tenant information
func (r *InMemoryTenantRegistry) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tenant, exists := r.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	// Return a copy
	tenantCopy := *tenant
	return &tenantCopy, nil
}

// ListTenants returns all tenants
func (r *InMemoryTenantRegistry) ListTenants(ctx context.Context) ([]*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(r.tenants))
	for _, tenant := range r.tenants {
		tenantCopy := *tenant
		tenants = append(tenants, &tenantCopy)
	}

	return tenants, nil
}

// ListTenantsByShard returns tenants on a specific shard
func (r *InMemoryTenantRegistry) ListTenantsByShard(ctx context.Context, shardID string) ([]*Tenant, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tenants []*Tenant
	for _, tenant := range r.tenants {
		if tenant.ShardID == shardID {
			tenantCopy := *tenant
			tenants = append(tenants, &tenantCopy)
		}
	}

	return tenants, nil
}

// RegisterTenant registers a new tenant
func (r *InMemoryTenantRegistry) RegisterTenant(ctx context.Context, tenant *Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if tenant.ID == "" {
		return fmt.Errorf("tenant ID cannot be empty")
	}

	if _, exists := r.tenants[tenant.ID]; exists {
		return fmt.Errorf("tenant %s already exists", tenant.ID)
	}

	if tenant.Status == "" {
		tenant.Status = "active"
	}

	tenant.CreatedAt = time.Now()
	tenant.UpdatedAt = time.Now()

	if tenant.Metadata == nil {
		tenant.Metadata = make(map[string]string)
	}

	r.tenants[tenant.ID] = tenant

	return nil
}

// UpdateTenant updates tenant information
func (r *InMemoryTenantRegistry) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tenants[tenant.ID]; !exists {
		return fmt.Errorf("tenant %s not found", tenant.ID)
	}

	tenant.UpdatedAt = time.Now()
	r.tenants[tenant.ID] = tenant

	return nil
}

// MoveTenant moves a tenant to a different shard
func (r *InMemoryTenantRegistry) MoveTenant(ctx context.Context, tenantID string, newShardID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tenant, exists := r.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	tenant.ShardID = newShardID
	tenant.UpdatedAt = time.Now()

	return nil
}

// DeleteTenant removes a tenant
func (r *InMemoryTenantRegistry) DeleteTenant(ctx context.Context, tenantID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tenants[tenantID]; !exists {
		return fmt.Errorf("tenant %s not found", tenantID)
	}

	delete(r.tenants, tenantID)

	return nil
}
