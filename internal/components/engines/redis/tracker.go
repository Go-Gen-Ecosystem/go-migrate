/**
 * @file tracker.go
 * @description Redis migration tracker implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/nosql/redis/tracker.go
 */

package redis

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	Database int
	SSL      bool
}

// RedisMigrationTracker implements MigrationTracker for Redis
type RedisMigrationTracker struct {
	client *redis.Client
	config *RedisConfig
}

// NewRedisMigrationTracker creates a new Redis migration tracker
func NewRedisMigrationTracker(config *RedisConfig) *RedisMigrationTracker {
	return &RedisMigrationTracker{
		config: config,
	}
}

// Connect establishes connection to Redis
func (t *RedisMigrationTracker) Connect(ctx context.Context) error {
	// Build Redis options
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%d", t.config.Host, t.config.Port),
		Password: t.config.Password,
		DB:       t.config.Database,
	}

	// SSL/TLS configuration
	if t.config.SSL {
		opts.TLSConfig = &tls.Config{
			InsecureSkipVerify: false,
		}
	}

	t.client = redis.NewClient(opts)

	// Test connection
	_, err := t.client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return nil
}

// Disconnect closes the Redis connection
func (t *RedisMigrationTracker) Disconnect(ctx context.Context) error {
	if t.client != nil {
		err := t.client.Close()
		t.client = nil
		return err
	}
	return nil
}

// EnsureSchemaCollection creates the migration tracking structures if they don't exist
func (t *RedisMigrationTracker) EnsureSchemaCollection(ctx context.Context) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	// Redis doesn't require explicit schema creation, but we can set up
	// some initial metadata about the migration system
	metadataKey := "migration:metadata"
	metadata := map[string]interface{}{
		"version":     "1.0",
		"created_at":  time.Now().Unix(),
		"initialized": true,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Set metadata only if it doesn't exist
	result, err := t.client.SetNX(ctx, metadataKey, metadataJSON, 0).Result()
	if err != nil {
		return fmt.Errorf("failed to set migration metadata: %w", err)
	}

	if result {
		// First time initialization - create indexes for efficient querying
		// Create a sorted set for migration versions
		_, err = t.client.ZAdd(ctx, "migration:versions", &redis.Z{
			Score:  0,
			Member: "initial",
		}).Result()
		if err != nil {
			return fmt.Errorf("failed to initialize migration versions: %w", err)
		}
	}

	return nil
}

// GetExecutedMigrations returns all executed migrations
func (t *RedisMigrationTracker) GetExecutedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Get all migration IDs with state "applied"
	pattern := "migration:*:state"
	keys, err := t.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration keys: %w", err)
	}

	var migrations []*types.Migration
	for _, key := range keys {
		state, err := t.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip if key doesn't exist
		}

		if state == "applied" {
			// Extract migration ID from key
			migrationID := t.extractMigrationIDFromKey(key)
			migration, err := t.GetMigrationByID(ctx, migrationID)
			if err != nil {
				continue // Skip if migration data is corrupted
			}
			migrations = append(migrations, migration)
		}
	}

	// Sort by executed_at
	return t.sortMigrationsByExecutedAt(migrations), nil
}

// GetFailedMigrations returns all failed migrations
func (t *RedisMigrationTracker) GetFailedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Get all migration IDs with state "failed"
	pattern := "migration:*:state"
	keys, err := t.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration keys: %w", err)
	}

	var migrations []*types.Migration
	for _, key := range keys {
		state, err := t.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip if key doesn't exist
		}

		if state == "failed" {
			// Extract migration ID from key
			migrationID := t.extractMigrationIDFromKey(key)
			migration, err := t.GetMigrationByID(ctx, migrationID)
			if err != nil {
				continue // Skip if migration data is corrupted
			}
			migrations = append(migrations, migration)
		}
	}

	// Sort by executed_at (most recent first)
	return t.sortMigrationsByExecutedAt(migrations), nil
}

// GetAppliedMigrations returns all applied migrations (executed + rolled back)
func (t *RedisMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Get all migration IDs with state "applied" or "rolled_back"
	pattern := "migration:*:state"
	keys, err := t.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration keys: %w", err)
	}

	var migrations []*types.Migration
	for _, key := range keys {
		state, err := t.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip if key doesn't exist
		}

		if state == "applied" || state == "rolled_back" {
			// Extract migration ID from key
			migrationID := t.extractMigrationIDFromKey(key)
			migration, err := t.GetMigrationByID(ctx, migrationID)
			if err != nil {
				continue // Skip if migration data is corrupted
			}
			migrations = append(migrations, migration)
		}
	}

	// Sort by executed_at
	return t.sortMigrationsByExecutedAt(migrations), nil
}

// RecordMigrationSuccess records a successful migration execution
func (t *RedisMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration *types.Migration) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	now := time.Now()
	migration.ExecutedAt = &now
	migration.State = "applied"
	migration.UpdatedAt = now

	// Store migration data in Redis using multiple keys for efficient querying
	migrationKey := fmt.Sprintf("migration:%s", migration.ID)
	
	// Store migration as JSON
	migrationJSON, err := json.Marshal(migration)
	if err != nil {
		return fmt.Errorf("failed to marshal migration: %w", err)
	}

	pipe := t.client.Pipeline()
	
	// Store the full migration data
	pipe.Set(ctx, migrationKey, migrationJSON, 0)
	
	// Store individual fields for efficient querying
	pipe.Set(ctx, fmt.Sprintf("%s:state", migrationKey), migration.State, 0)
	pipe.Set(ctx, fmt.Sprintf("%s:version", migrationKey), migration.Version, 0)
	pipe.Set(ctx, fmt.Sprintf("%s:executed_at", migrationKey), migration.ExecutedAt.Unix(), 0)
	
	// Add to sorted set for version ordering
	score := float64(migration.ExecutedAt.Unix())
	pipe.ZAdd(ctx, "migration:versions", &redis.Z{Score: score, Member: migration.ID})
	
	// Add to applied migrations set
	pipe.SAdd(ctx, "migration:applied", migration.ID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to record migration success: %w", err)
	}

	return nil
}

// RecordMigrationFailure records a failed migration execution
func (t *RedisMigrationTracker) RecordMigrationFailure(ctx context.Context, migration *types.Migration, errorMsg string) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	now := time.Now()
	migration.ExecutedAt = &now
	migration.State = "failed"
	migration.ErrorMessage = errorMsg
	migration.UpdatedAt = now

	// Store migration data in Redis
	migrationKey := fmt.Sprintf("migration:%s", migration.ID)
	
	// Store migration as JSON
	migrationJSON, err := json.Marshal(migration)
	if err != nil {
		return fmt.Errorf("failed to marshal migration: %w", err)
	}

	pipe := t.client.Pipeline()
	
	// Store the full migration data
	pipe.Set(ctx, migrationKey, migrationJSON, 0)
	
	// Store individual fields for efficient querying
	pipe.Set(ctx, fmt.Sprintf("%s:state", migrationKey), migration.State, 0)
	pipe.Set(ctx, fmt.Sprintf("%s:version", migrationKey), migration.Version, 0)
	pipe.Set(ctx, fmt.Sprintf("%s:executed_at", migrationKey), migration.ExecutedAt.Unix(), 0)
	pipe.Set(ctx, fmt.Sprintf("%s:error", migrationKey), errorMsg, 0)
	
	// Add to failed migrations set
	pipe.SAdd(ctx, "migration:failed", migration.ID)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to record migration failure: %w", err)
	}

	return nil
}

// RemoveMigration removes a migration record
func (t *RedisMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	migrationKey := fmt.Sprintf("migration:%s", migrationID)
	
	pipe := t.client.Pipeline()
	
	// Remove all migration-related keys
	pipe.Del(ctx, migrationKey)
	pipe.Del(ctx, fmt.Sprintf("%s:state", migrationKey))
	pipe.Del(ctx, fmt.Sprintf("%s:version", migrationKey))
	pipe.Del(ctx, fmt.Sprintf("%s:executed_at", migrationKey))
	pipe.Del(ctx, fmt.Sprintf("%s:error", migrationKey))
	
	// Remove from sets and sorted sets
	pipe.ZRem(ctx, "migration:versions", migrationID)
	pipe.SRem(ctx, "migration:applied", migrationID)
	pipe.SRem(ctx, "migration:failed", migrationID)

	results, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove migration: %w", err)
	}

	// Check if the main migration key was actually deleted
	if results[0].(*redis.IntCmd).Val() == 0 {
		return fmt.Errorf("migration %s not found", migrationID)
	}

	return nil
}

// IsLocked checks if migrations are currently locked
func (t *RedisMigrationTracker) IsLocked(ctx context.Context) (bool, error) {
	if t.client == nil {
		return false, fmt.Errorf("not connected to Redis")
	}

	lockKey := "migration:lock"
	
	// Check if lock exists and is not expired
	ttl, err := t.client.TTL(ctx, lockKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	// If TTL is -2, key doesn't exist (not locked)
	// If TTL is -1, key exists but has no expiration (locked indefinitely)
	// If TTL > 0, key exists and will expire (locked temporarily)
	return ttl != -2*time.Second, nil
}

// AcquireLock acquires a migration lock
func (t *RedisMigrationTracker) AcquireLock(ctx context.Context, timeout time.Duration) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	lockKey := "migration:lock"
	hostname := getHostname()
	processID := getProcessID()
	
	lockValue := fmt.Sprintf("%s:%d:%d", hostname, processID, time.Now().Unix())

	// Try to acquire lock using SET with NX and EX options
	result, err := t.client.SetNX(ctx, lockKey, lockValue, timeout).Result()
	if err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}

	if !result {
		// Lock is already held, get lock info
		currentLock, err := t.client.Get(ctx, lockKey).Result()
		if err != nil {
			return fmt.Errorf("migration lock is already held by another process")
		}
		return fmt.Errorf("migration lock is already held by: %s", currentLock)
	}

	return nil
}

// ReleaseLock releases the migration lock
func (t *RedisMigrationTracker) ReleaseLock(ctx context.Context) error {
	if t.client == nil {
		return fmt.Errorf("not connected to Redis")
	}

	lockKey := "migration:lock"
	hostname := getHostname()
	processID := getProcessID()
	
	// Use Lua script to atomically check and delete the lock
	luaScript := `
		local lockKey = KEYS[1]
		local expectedValue = ARGV[1]
		local currentValue = redis.call('GET', lockKey)
		
		if currentValue == false then
			return 0  -- Lock doesn't exist
		end
		
		-- Check if the lock belongs to this process
		if string.find(currentValue, expectedValue) == 1 then
			redis.call('DEL', lockKey)
			return 1  -- Successfully released
		else
			return -1  -- Lock belongs to another process
		end
	`
	
	expectedPrefix := fmt.Sprintf("%s:%d:", hostname, processID)
	result, err := t.client.Eval(ctx, luaScript, []string{lockKey}, expectedPrefix).Result()
	if err != nil {
		return fmt.Errorf("failed to release migration lock: %w", err)
	}

	switch result.(int64) {
	case 0:
		return fmt.Errorf("migration lock not found")
	case -1:
		return fmt.Errorf("migration lock not owned by this process")
	case 1:
		return nil // Successfully released
	default:
		return fmt.Errorf("unexpected result from lock release script: %v", result)
	}
}

// GetCurrentVersion returns the current migration version
func (t *RedisMigrationTracker) GetCurrentVersion(ctx context.Context) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("not connected to Redis")
	}

	// Get the latest applied migration from the sorted set
	results, err := t.client.ZRevRange(ctx, "migration:versions", 0, 0).Result()
	if err != nil {
		return "", fmt.Errorf("failed to get current version: %w", err)
	}

	if len(results) == 0 {
		return "", nil // No migrations applied yet
	}

	// Get the version of the latest migration
	migrationID := results[0]
	if migrationID == "initial" {
		return "", nil
	}

	versionKey := fmt.Sprintf("migration:%s:version", migrationID)
	version, err := t.client.Get(ctx, versionKey).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, nil
}

// IsMigrationApplied checks if a specific migration is applied
func (t *RedisMigrationTracker) IsMigrationApplied(ctx context.Context, migrationID string) (bool, error) {
	if t.client == nil {
		return false, fmt.Errorf("not connected to Redis")
	}

	stateKey := fmt.Sprintf("migration:%s:state", migrationID)
	state, err := t.client.Get(ctx, stateKey).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil // Migration not found
		}
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return state == "applied", nil
}

// GetMigrationByID returns a specific migration by ID
func (t *RedisMigrationTracker) GetMigrationByID(ctx context.Context, migrationID string) (*types.Migration, error) {
	if t.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	migrationKey := fmt.Sprintf("migration:%s", migrationID)
	migrationJSON, err := t.client.Get(ctx, migrationKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("migration %s not found", migrationID)
		}
		return nil, fmt.Errorf("failed to get migration: %w", err)
	}

	var migration types.Migration
	if err := json.Unmarshal([]byte(migrationJSON), &migration); err != nil {
		return nil, fmt.Errorf("failed to unmarshal migration: %w", err)
	}

	return &migration, nil
}

// GetMigrationByVersion returns a specific migration by version
func (t *RedisMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	if t.client == nil {
		return nil, fmt.Errorf("not connected to Redis")
	}

	// Search for migration with the specified version
	pattern := "migration:*:version"
	keys, err := t.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to search for migration version: %w", err)
	}

	for _, key := range keys {
		versionValue, err := t.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		if versionValue == version {
			// Extract migration ID from key
			migrationID := t.extractMigrationIDFromKey(key)
			return t.GetMigrationByID(ctx, migrationID)
		}
	}

	return nil, fmt.Errorf("migration with version %s not found", version)
}

// Helper functions

// extractMigrationIDFromKey extracts migration ID from Redis key
func (t *RedisMigrationTracker) extractMigrationIDFromKey(key string) string {
	// Key format: "migration:ID:field"
	parts := strings.Split(key, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// sortMigrationsByExecutedAt sorts migrations by executed_at timestamp
func (t *RedisMigrationTracker) sortMigrationsByExecutedAt(migrations []*types.Migration) []*types.Migration {
	// Simple bubble sort - in production, use a more efficient algorithm
	for i := 0; i < len(migrations)-1; i++ {
		for j := 0; j < len(migrations)-i-1; j++ {
			var time1, time2 time.Time
			
			if migrations[j].ExecutedAt != nil {
				time1 = *migrations[j].ExecutedAt
			}
			if migrations[j+1].ExecutedAt != nil {
				time2 = *migrations[j+1].ExecutedAt
			}
			
			if time1.After(time2) {
				migrations[j], migrations[j+1] = migrations[j+1], migrations[j]
			}
		}
	}
	return migrations
}

// getHostname returns the hostname of the current machine
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getProcessID returns the process ID of the current process
func getProcessID() int {
	return os.Getpid()
}
