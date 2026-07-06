/**
 * @file tracker.go
 * @description MongoDB migration tracker implementation
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/tracker.go
 */

package mongodb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MongoMigrationTracker implements NoSQLMigrationTracker for MongoDB
type MongoMigrationTracker struct {
	client     *mongo.Client
	database   *mongo.Database
	config     *types.NoSQLConnectionConfig
	collection *mongo.Collection
	lockColl   *mongo.Collection
}

// NewMongoMigrationTracker creates a new MongoDB migration tracker
func NewMongoMigrationTracker(config *types.NoSQLConnectionConfig) *MongoMigrationTracker {
	return &MongoMigrationTracker{
		config: config,
	}
}

// Connect establishes connection to MongoDB
func (t *MongoMigrationTracker) Connect(ctx context.Context) error {
	// Build connection URI
	uri := t.buildConnectionURI()
	
	clientOptions := options.Client().ApplyURI(uri)
	
	// Set connection timeouts
	clientOptions.SetConnectTimeout(t.config.ConnectTimeout)
	clientOptions.SetSocketTimeout(t.config.SocketTimeout)
	clientOptions.SetMaxPoolSize(uint64(t.config.MaxConnections))
	clientOptions.SetMinPoolSize(uint64(t.config.MinConnections))
	clientOptions.SetMaxConnIdleTime(t.config.MaxIdleTime)
	clientOptions.SetHeartbeatInterval(t.config.HeartbeatInterval)

	// SSL configuration
	if t.config.SSL {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}
		
		// Load CA certificate if provided
		if t.config.SSLCAPath != "" {
			caCert, err := os.ReadFile(t.config.SSLCAPath)
			if err != nil {
				return fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return fmt.Errorf("failed to parse CA certificate")
			}
			tlsConfig.RootCAs = caCertPool
		}
		
		// Load client certificate if provided
		if t.config.SSLCertPath != "" && t.config.SSLKeyPath != "" {
			cert, err := tls.LoadX509KeyPair(t.config.SSLCertPath, t.config.SSLKeyPath)
			if err != nil {
				return fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		
		clientOptions.SetTLSConfig(tlsConfig)
	}

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	t.client = client
	t.database = client.Database(t.config.Database)
	t.collection = t.database.Collection("schema_migrations")
	t.lockColl = t.database.Collection("migration_locks")

	return nil
}

// Disconnect closes the MongoDB connection
func (t *MongoMigrationTracker) Disconnect(ctx context.Context) error {
	if t.client != nil {
		err := t.client.Disconnect(ctx)
		t.client = nil
		t.database = nil
		t.collection = nil
		t.lockColl = nil
		return err
	}
	return nil
}

// EnsureSchemaCollection creates the migration tracking collection if it doesn't exist
func (t *MongoMigrationTracker) EnsureSchemaCollection(ctx context.Context) error {
	if t.database == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	// Create migrations collection with indexes
	err := t.database.CreateCollection(ctx, "schema_migrations")
	if err != nil {
		// Collection might already exist, which is fine
		if !mongo.IsDuplicateKeyError(err) && err.Error() != "collection already exists" {
			return fmt.Errorf("failed to create schema_migrations collection: %w", err)
		}
	}

	// Create indexes for efficient querying
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "migration_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "version", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "executed_at", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "state", Value: 1}},
		},
	}

	_, err = t.collection.Indexes().CreateMany(ctx, indexes)
	if err != nil {
		return fmt.Errorf("failed to create indexes on schema_migrations: %w", err)
	}

	// Create locks collection with TTL index
	err = t.database.CreateCollection(ctx, "migration_locks")
	if err != nil {
		// Collection might already exist, which is fine
		if !mongo.IsDuplicateKeyError(err) && err.Error() != "collection already exists" {
			return fmt.Errorf("failed to create migration_locks collection: %w", err)
		}
	}

	// Create indexes for locks collection
	lockIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "lock_name", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0), // TTL index
		},
	}

	_, err = t.lockColl.Indexes().CreateMany(ctx, lockIndexes)
	if err != nil {
		return fmt.Errorf("failed to create indexes on migration_locks: %w", err)
	}

	return nil
}

// GetExecutedMigrations returns all executed migrations
func (t *MongoMigrationTracker) GetExecutedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.collection == nil {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "state", Value: "applied"}}
	opts := options.Find().SetSort(bson.D{{Key: "executed_at", Value: 1}})

	cursor, err := t.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query executed migrations: %w", err)
	}
	defer cursor.Close(ctx)

	var migrations []*types.Migration
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode migration document: %w", err)
		}

		migration := t.documentToMigration(doc)
		migrations = append(migrations, migration)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return migrations, nil
}

// GetFailedMigrations returns all failed migrations
func (t *MongoMigrationTracker) GetFailedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.collection == nil {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "state", Value: "failed"}}
	opts := options.Find().SetSort(bson.D{{Key: "executed_at", Value: -1}})

	cursor, err := t.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query failed migrations: %w", err)
	}
	defer cursor.Close(ctx)

	var migrations []*types.Migration
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode migration document: %w", err)
		}

		migration := t.documentToMigration(doc)
		migrations = append(migrations, migration)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return migrations, nil
}

// GetAppliedMigrations returns all applied migrations (executed + rolled back)
func (t *MongoMigrationTracker) GetAppliedMigrations(ctx context.Context) ([]*types.Migration, error) {
	if t.collection == nil {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "state", Value: bson.D{{Key: "$in", Value: []string{"applied", "rolled_back"}}}}}
	opts := options.Find().SetSort(bson.D{{Key: "executed_at", Value: 1}})

	cursor, err := t.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer cursor.Close(ctx)

	var migrations []*types.Migration
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode migration document: %w", err)
		}

		migration := t.documentToMigration(doc)
		migrations = append(migrations, migration)
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return migrations, nil
}

// RecordMigrationSuccess records a successful migration execution
func (t *MongoMigrationTracker) RecordMigrationSuccess(ctx context.Context, migration *types.Migration) error {
	if t.collection == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	now := time.Now()
	migration.ExecutedAt = &now
	migration.State = "applied"
	migration.UpdatedAt = now

	doc := t.migrationToDocument(migration)
	
	filter := bson.D{{Key: "migration_id", Value: migration.ID}}
	update := bson.D{{Key: "$set", Value: doc}}
	opts := options.Update().SetUpsert(true)

	_, err := t.collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to record migration success: %w", err)
	}

	return nil
}

// RecordMigrationFailure records a failed migration execution
func (t *MongoMigrationTracker) RecordMigrationFailure(ctx context.Context, migration *types.Migration, errorMsg string) error {
	if t.collection == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	now := time.Now()
	migration.ExecutedAt = &now
	migration.State = "failed"
	migration.ErrorMessage = errorMsg
	migration.UpdatedAt = now

	doc := t.migrationToDocument(migration)
	
	filter := bson.D{{Key: "migration_id", Value: migration.ID}}
	update := bson.D{{Key: "$set", Value: doc}}
	opts := options.Update().SetUpsert(true)

	_, err := t.collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to record migration failure: %w", err)
	}

	return nil
}

// RemoveMigration removes a migration record
func (t *MongoMigrationTracker) RemoveMigration(ctx context.Context, migrationID string) error {
	if t.collection == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "migration_id", Value: migrationID}}
	
	result, err := t.collection.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to remove migration: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("migration %s not found", migrationID)
	}

	return nil
}

// IsLocked checks if migrations are currently locked
func (t *MongoMigrationTracker) IsLocked(ctx context.Context) (bool, error) {
	if t.lockColl == nil {
		return false, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{
		{Key: "lock_name", Value: "migration_lock"},
		{Key: "expires_at", Value: bson.D{{Key: "$gt", Value: time.Now()}}},
	}

	count, err := t.lockColl.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check lock status: %w", err)
	}

	return count > 0, nil
}

// AcquireLock acquires a migration lock
func (t *MongoMigrationTracker) AcquireLock(ctx context.Context, timeout time.Duration) error {
	if t.lockColl == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	hostname := getHostname()
	processID := getProcessID()
	expiresAt := time.Now().Add(timeout)

	lockDoc := bson.D{
		{Key: "lock_name", Value: "migration_lock"},
		{Key: "hostname", Value: hostname},
		{Key: "process_id", Value: processID},
		{Key: "acquired_at", Value: time.Now()},
		{Key: "expires_at", Value: expiresAt},
	}

	// Try to insert the lock document
	_, err := t.lockColl.InsertOne(ctx, lockDoc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return fmt.Errorf("migration lock is already held by another process")
		}
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}

	return nil
}

// ReleaseLock releases the migration lock
func (t *MongoMigrationTracker) ReleaseLock(ctx context.Context) error {
	if t.lockColl == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	hostname := getHostname()
	processID := getProcessID()

	filter := bson.D{
		{Key: "lock_name", Value: "migration_lock"},
		{Key: "hostname", Value: hostname},
		{Key: "process_id", Value: processID},
	}

	result, err := t.lockColl.DeleteOne(ctx, filter)
	if err != nil {
		return fmt.Errorf("failed to release migration lock: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("migration lock not found or not owned by this process")
	}

	return nil
}

// GetCurrentVersion returns the current migration version
func (t *MongoMigrationTracker) GetCurrentVersion(ctx context.Context) (string, error) {
	if t.collection == nil {
		return "", fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "state", Value: "applied"}}
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})

	var doc bson.M
	err := t.collection.FindOne(ctx, filter, opts).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", nil // No migrations applied yet
		}
		return "", fmt.Errorf("failed to get current version: %w", err)
	}

	version, ok := doc["version"].(string)
	if !ok {
		return "", fmt.Errorf("invalid version format in database")
	}

	return version, nil
}

// IsMigrationApplied checks if a specific migration is applied
func (t *MongoMigrationTracker) IsMigrationApplied(ctx context.Context, migrationID string) (bool, error) {
	if t.collection == nil {
		return false, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{
		{Key: "migration_id", Value: migrationID},
		{Key: "state", Value: "applied"},
	}

	count, err := t.collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return count > 0, nil
}

// GetMigrationByID returns a specific migration by ID
func (t *MongoMigrationTracker) GetMigrationByID(ctx context.Context, migrationID string) (*types.Migration, error) {
	if t.collection == nil {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "migration_id", Value: migrationID}}

	var doc bson.M
	err := t.collection.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("migration %s not found", migrationID)
		}
		return nil, fmt.Errorf("failed to get migration: %w", err)
	}

	migration := t.documentToMigration(doc)
	return migration, nil
}

// GetMigrationByVersion returns a specific migration by version
func (t *MongoMigrationTracker) GetMigrationByVersion(ctx context.Context, version string) (*types.Migration, error) {
	if t.collection == nil {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	filter := bson.D{{Key: "version", Value: version}}

	var doc bson.M
	err := t.collection.FindOne(ctx, filter).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("migration with version %s not found", version)
		}
		return nil, fmt.Errorf("failed to get migration: %w", err)
	}

	migration := t.documentToMigration(doc)
	return migration, nil
}

// migrationToDocument converts a Migration to a MongoDB document
func (t *MongoMigrationTracker) migrationToDocument(migration *types.Migration) bson.D {
	doc := bson.D{
		{Key: "migration_id", Value: migration.ID},
		{Key: "version", Value: migration.Version},
		{Key: "name", Value: migration.Description},
		{Key: "description", Value: migration.Description},
		{Key: "script", Value: migration.Script},
		{Key: "rollback_sql", Value: migration.RollbackSQL},
		{Key: "checksum", Value: migration.Checksum},
		{Key: "state", Value: migration.State},
		{Key: "error_message", Value: migration.ErrorMessage},
		{Key: "created_at", Value: migration.CreatedAt},
		{Key: "updated_at", Value: migration.UpdatedAt},
	}

	if migration.ExecutedAt != nil {
		doc = append(doc, bson.E{Key: "executed_at", Value: *migration.ExecutedAt})
	}

	if migration.RolledBackAt != nil {
		doc = append(doc, bson.E{Key: "rolled_back_at", Value: *migration.RolledBackAt})
	}

	if migration.ExecutionTime > 0 {
		doc = append(doc, bson.E{Key: "execution_time_ms", Value: migration.ExecutionTime.Milliseconds()})
	}

	return doc
}

// documentToMigration converts a MongoDB document to a Migration
func (t *MongoMigrationTracker) documentToMigration(doc bson.M) *types.Migration {
	migration := &types.Migration{}

	if id, ok := doc["migration_id"].(string); ok {
		migration.ID = id
	}

	if version, ok := doc["version"].(string); ok {
		migration.Version = version
	}

	if name, ok := doc["name"].(string); ok {
		migration.Description = name
	}

	if description, ok := doc["description"].(string); ok {
		migration.Description = description
	}

	if script, ok := doc["script"].(string); ok {
		migration.Script = script
	}

	if rollbackSQL, ok := doc["rollback_sql"].(string); ok {
		migration.RollbackSQL = rollbackSQL
	}

	if checksum, ok := doc["checksum"].(string); ok {
		migration.Checksum = checksum
	}

	if state, ok := doc["state"].(string); ok {
		migration.State = state
	}

	if errorMessage, ok := doc["error_message"].(string); ok {
		migration.ErrorMessage = errorMessage
	}

	if createdAt, ok := doc["created_at"].(primitive.DateTime); ok {
		migration.CreatedAt = createdAt.Time()
	}

	if updatedAt, ok := doc["updated_at"].(primitive.DateTime); ok {
		migration.UpdatedAt = updatedAt.Time()
	}

	if executedAt, ok := doc["executed_at"].(primitive.DateTime); ok {
		t := executedAt.Time()
		migration.ExecutedAt = &t
	}

	if rolledBackAt, ok := doc["rolled_back_at"].(primitive.DateTime); ok {
		t := rolledBackAt.Time()
		migration.RolledBackAt = &t
	}

	if executionTimeMs, ok := doc["execution_time_ms"].(int64); ok {
		migration.ExecutionTime = time.Duration(executionTimeMs) * time.Millisecond
	}

	return migration
}

// buildConnectionURI builds MongoDB connection URI
func (t *MongoMigrationTracker) buildConnectionURI() string {
	// Reuse the same logic from executor
	executor := &MongoMigrationExecutor{config: t.config}
	return executor.buildConnectionURI()
}

// Helper functions
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getProcessID() int {
	return os.Getpid()
}
