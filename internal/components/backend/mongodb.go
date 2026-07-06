/**
 * @file mongodb.go
 * @description MongoDB backend implementation using Bridge pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/backend/mongodb.go
 */

package backend

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// MongoDBBackend implements DatabaseBackend for MongoDB
type MongoDBBackend struct {
	client    *mongo.Client
	database  *mongo.Database
	config    types.DatabaseConfig
	connected bool
}

// NewMongoDBBackend creates a new MongoDB backend
func NewMongoDBBackend(config types.DatabaseConfig) (DatabaseBackend, error) {
	return &MongoDBBackend{
		config: config,
	}, nil
}

// Connect establishes a connection to MongoDB
func (m *MongoDBBackend) Connect(ctx context.Context, config types.DatabaseConfig) error {
	if m.connected {
		return nil
	}

	uri := m.buildURI(config)
	clientOptions := options.Client().ApplyURI(uri)

	// Configure connection pool
	if config.MaxConnections > 0 {
		maxPoolSize := uint64(config.MaxConnections)
		clientOptions.SetMaxPoolSize(maxPoolSize)
	}
	if config.MaxConnections > 0 {
		minPoolSize := uint64(config.MaxConnections)
		clientOptions.SetMinPoolSize(minPoolSize)
	}
	if config.MaxLifetime > 0 {
		maxConnIdleTime := config.MaxLifetime
		clientOptions.SetMaxConnIdleTime(maxConnIdleTime)
	}

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Test connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	m.client = client
	m.database = client.Database(config.Database)
	m.config = config
	m.connected = true

	return nil
}

// Disconnect closes the MongoDB connection
func (m *MongoDBBackend) Disconnect(ctx context.Context) error {
	if !m.connected || m.client == nil {
		return nil
	}

	err := m.client.Disconnect(ctx)
	m.connected = false
	m.client = nil
	m.database = nil

	return err
}

// Ping checks the MongoDB connection
func (m *MongoDBBackend) Ping(ctx context.Context) error {
	if !m.connected || m.client == nil {
		return fmt.Errorf("not connected to MongoDB")
	}

	return m.client.Ping(ctx, nil)
}

// IsConnected returns connection status
func (m *MongoDBBackend) IsConnected() bool {
	return m.connected && m.client != nil
}

// BeginTransaction starts a new transaction (MongoDB sessions)
func (m *MongoDBBackend) BeginTransaction(ctx context.Context) (Transaction, error) {
	return m.BeginTransactionWithOptions(ctx, TransactionOptions{})
}

// BeginTransactionWithOptions starts a new transaction with options
func (m *MongoDBBackend) BeginTransactionWithOptions(ctx context.Context, opts TransactionOptions) (Transaction, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	session, err := m.client.StartSession()
	if err != nil {
		return nil, fmt.Errorf("failed to start MongoDB session: %w", err)
	}

	// Start transaction
	err = session.StartTransaction()
	if err != nil {
		session.EndSession(ctx)
		return nil, fmt.Errorf("failed to start MongoDB transaction: %w", err)
	}

	return &MongoDBTransaction{
		session:  session,
		database: m.database,
		ctx:      ctx,
	}, nil
}

// Execute executes a MongoDB operation (simulated as SQL for compatibility)
func (m *MongoDBBackend) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	// Parse and execute MongoDB operations
	operation, err := m.parseOperation(query, args...)
	if err != nil {
		return nil, err
	}

	result, err := m.executeOperation(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("failed to execute MongoDB operation: %w", err)
	}

	return &MongoDBQueryResult{
		modifiedCount: result.ModifiedCount,
		insertedCount: result.InsertedCount,
		deletedCount:  result.DeletedCount,
	}, nil
}

// Query executes a query that returns documents
func (m *MongoDBBackend) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	if !m.connected {
		return nil, fmt.Errorf("not connected to MongoDB")
	}

	operation, err := m.parseOperation(query, args...)
	if err != nil {
		return nil, err
	}

	cursor, err := m.executeQuery(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("failed to query MongoDB: %w", err)
	}

	return &MongoDBQueryRows{cursor: cursor, ctx: ctx}, nil
}

// QueryRow executes a query that returns a single document
func (m *MongoDBBackend) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	if !m.connected {
		return &MongoDBQueryRow{err: fmt.Errorf("not connected to MongoDB")}
	}

	operation, err := m.parseOperation(query, args...)
	if err != nil {
		return &MongoDBQueryRow{err: err}
	}

	result := m.database.Collection(operation.Collection).FindOne(ctx, operation.Filter)
	return &MongoDBQueryRow{result: result}
}

// CreateSchema creates a new database (MongoDB doesn't have schemas)
func (m *MongoDBBackend) CreateSchema(ctx context.Context, schemaName string) error {
	// MongoDB creates databases implicitly
	return nil
}

// DropSchema drops a database
func (m *MongoDBBackend) DropSchema(ctx context.Context, schemaName string) error {
	if schemaName == m.config.Database {
		return m.database.Drop(ctx)
	}

	// Drop different database
	db := m.client.Database(schemaName)
	return db.Drop(ctx)
}

// SchemaExists checks if a database exists
func (m *MongoDBBackend) SchemaExists(ctx context.Context, schemaName string) (bool, error) {
	databases, err := m.client.ListDatabaseNames(ctx, bson.M{"name": schemaName})
	if err != nil {
		return false, err
	}

	return len(databases) > 0, nil
}

// ListSchemas returns all databases
func (m *MongoDBBackend) ListSchemas(ctx context.Context) ([]string, error) {
	return m.client.ListDatabaseNames(ctx, bson.M{})
}

// CreateTable creates a new collection
func (m *MongoDBBackend) CreateTable(ctx context.Context, tableDef TableDefinition) error {
	// MongoDB creates collections implicitly, but we can create with options
	opts := options.CreateCollection()

	// Set validation if provided
	if len(tableDef.Constraints) > 0 {
		validator := m.buildValidator(tableDef.Constraints)
		opts.SetValidator(validator)
	}

	return m.database.CreateCollection(ctx, tableDef.Name, opts)
}

// DropTable drops a collection
func (m *MongoDBBackend) DropTable(ctx context.Context, tableName string) error {
	return m.database.Collection(tableName).Drop(ctx)
}

// TableExists checks if a collection exists
func (m *MongoDBBackend) TableExists(ctx context.Context, tableName string) (bool, error) {
	collections, err := m.database.ListCollectionNames(ctx, bson.M{"name": tableName})
	if err != nil {
		return false, err
	}

	return len(collections) > 0, nil
}

// ListTables returns all collections
func (m *MongoDBBackend) ListTables(ctx context.Context) ([]string, error) {
	return m.database.ListCollectionNames(ctx, bson.M{})
}

// GetTableInfo returns collection information
func (m *MongoDBBackend) GetTableInfo(ctx context.Context, tableName string) (TableInfo, error) {
	info := TableInfo{
		Name:   tableName,
		Type:   "collection",
		Schema: m.config.Database,
	}

	// Get collection stats
	var result bson.M
	err := m.database.RunCommand(ctx, bson.D{{Key: "collStats", Value: tableName}}).Decode(&result)
	if err != nil {
		return info, err
	}

	// MongoDB collections don't have fixed schemas, so we can't list columns
	// We could sample documents to infer schema, but that's expensive

	return info, nil
}

// CreateIndex creates an index
func (m *MongoDBBackend) CreateIndex(ctx context.Context, indexDef IndexDefinition) error {
	collection := m.database.Collection(indexDef.Table)

	// Build index keys
	keys := bson.D{}
	for _, column := range indexDef.Columns {
		keys = append(keys, bson.E{Key: column, Value: 1}) // 1 for ascending
	}

	// Build index options
	opts := options.Index()
	opts.SetName(indexDef.Name)
	opts.SetUnique(indexDef.Unique)

	indexModel := mongo.IndexModel{
		Keys:    keys,
		Options: opts,
	}

	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	return err
}

// DropIndex drops an index
func (m *MongoDBBackend) DropIndex(ctx context.Context, indexName string) error {
	// We need to know which collection the index belongs to
	// For now, we'll search all collections
	collections, err := m.ListTables(ctx)
	if err != nil {
		return err
	}

	for _, collName := range collections {
		collection := m.database.Collection(collName)
		_, err := collection.Indexes().DropOne(ctx, indexName)
		if err == nil {
			return nil // Found and dropped
		}
	}

	return fmt.Errorf("index %s not found", indexName)
}

// IndexExists checks if an index exists
func (m *MongoDBBackend) IndexExists(ctx context.Context, indexName string) (bool, error) {
	collections, err := m.ListTables(ctx)
	if err != nil {
		return false, err
	}

	for _, collName := range collections {
		collection := m.database.Collection(collName)
		cursor, err := collection.Indexes().List(ctx)
		if err != nil {
			continue
		}

		var indexes []bson.M
		if err := cursor.All(ctx, &indexes); err != nil {
			continue
		}

		for _, index := range indexes {
			if name, ok := index["name"].(string); ok && name == indexName {
				return true, nil
			}
		}
	}

	return false, nil
}

// ListIndexes returns all indexes for a collection
func (m *MongoDBBackend) ListIndexes(ctx context.Context, tableName string) ([]IndexInfo, error) {
	collection := m.database.Collection(tableName)
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}

	var mongoIndexes []bson.M
	if err := cursor.All(ctx, &mongoIndexes); err != nil {
		return nil, err
	}

	var indexes []IndexInfo
	for _, mongoIndex := range mongoIndexes {
		name, _ := mongoIndex["name"].(string)
		unique := false
		if u, ok := mongoIndex["unique"].(bool); ok {
			unique = u
		}

		indexes = append(indexes, IndexInfo{
			Name:   name,
			Table:  tableName,
			Unique: unique,
		})
	}

	return indexes, nil
}

// AddConstraint adds a constraint (MongoDB validation rules)
func (m *MongoDBBackend) AddConstraint(ctx context.Context, constraintDef ConstraintDefinition) error {
	// MongoDB uses validation rules instead of traditional constraints
	validator := m.buildConstraintValidator(constraintDef)

	command := bson.D{
		{Key: "collMod", Value: constraintDef.Table},
		{Key: "validator", Value: validator},
	}

	return m.database.RunCommand(ctx, command).Err()
}

// DropConstraint drops a constraint
func (m *MongoDBBackend) DropConstraint(ctx context.Context, tableName, constraintName string) error {
	// MongoDB doesn't have named constraints like SQL databases
	// We would need to modify the validator to remove specific rules
	return fmt.Errorf("MongoDB doesn't support named constraints")
}

// ListConstraints returns all constraints for a collection
func (m *MongoDBBackend) ListConstraints(ctx context.Context, tableName string) ([]ConstraintInfo, error) {
	// MongoDB validation rules are not easily enumerable as individual constraints
	return []ConstraintInfo{}, nil
}

// GetDatabaseInfo returns database information
func (m *MongoDBBackend) GetDatabaseInfo(ctx context.Context) (DatabaseInfo, error) {
	version, err := m.GetVersion(ctx)
	if err != nil {
		return DatabaseInfo{}, err
	}

	// Get database stats
	var stats bson.M
	err = m.database.RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&stats)
	if err != nil {
		return DatabaseInfo{}, err
	}

	size := int64(0)
	if dataSize, ok := stats["dataSize"].(int64); ok {
		size = dataSize
	}

	return DatabaseInfo{
		Name:    m.config.Database,
		Version: version,
		Dialect: DialectMongoDB,
		Size:    size,
	}, nil
}

// GetVersion returns MongoDB version
func (m *MongoDBBackend) GetVersion(ctx context.Context) (string, error) {
	var result bson.M
	err := m.database.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&result)
	if err != nil {
		return "", err
	}

	if version, ok := result["version"].(string); ok {
		return version, nil
	}

	return "unknown", nil
}

// GetDialect returns the database dialect
func (m *MongoDBBackend) GetDialect() DatabaseDialect {
	return DialectMongoDB
}

// AdaptSQL adapts SQL for MongoDB (converts to MongoDB operations)
func (m *MongoDBBackend) AdaptSQL(sql string) string {
	// This is a simplified conversion - in practice, you'd need a full SQL parser
	sql = strings.TrimSpace(sql)

	// Basic conversions
	sql = strings.ReplaceAll(sql, "SELECT * FROM", "db.{collection}.find()")
	sql = strings.ReplaceAll(sql, "INSERT INTO", "db.{collection}.insertOne()")
	sql = strings.ReplaceAll(sql, "UPDATE", "db.{collection}.updateMany()")
	sql = strings.ReplaceAll(sql, "DELETE FROM", "db.{collection}.deleteMany()")

	return sql
}

// ValidateSQL validates operations for MongoDB
func (m *MongoDBBackend) ValidateSQL(sql string) error {
	// Basic validation for MongoDB operations
	sql = strings.TrimSpace(strings.ToUpper(sql))

	// Check for dangerous operations
	dangerousPatterns := []string{
		"DROP DATABASE",
		"db\\..*\\.drop\\(\\)",
		"rs\\.initiate",
		"sh\\.enableSharding",
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, sql); matched {
			return fmt.Errorf("dangerous MongoDB operation detected: %s", pattern)
		}
	}

	return nil
}

// CreateMigrationTable creates the migration tracking collection
func (m *MongoDBBackend) CreateMigrationTable(ctx context.Context, tableName string) error {
	// Check if collection exists first
	exists, err := m.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Create collection with validation
	validator := bson.M{
		"$jsonSchema": bson.M{
			"bsonType": "object",
			"required": []string{"version", "type"},
			"properties": bson.M{
				"version": bson.M{
					"bsonType":    "string",
					"description": "Migration version",
				},
				"description": bson.M{
					"bsonType":    "string",
					"description": "Migration description",
				},
				"type": bson.M{
					"bsonType":    "string",
					"description": "Migration type",
				},
				"installed_by": bson.M{
					"bsonType":    "string",
					"description": "User who installed the migration",
				},
				"installed_on": bson.M{
					"bsonType":    "date",
					"description": "Installation timestamp",
				},
				"execution_time": bson.M{
					"bsonType":    "long",
					"description": "Execution time in milliseconds",
				},
				"success": bson.M{
					"bsonType":    "bool",
					"description": "Success flag",
				},
				"checksum": bson.M{
					"bsonType":    "string",
					"description": "Migration checksum",
				},
			},
		},
	}

	opts := options.CreateCollection().SetValidator(validator)
	err = m.database.CreateCollection(ctx, tableName, opts)
	if err != nil {
		return err
	}

	// Create unique index on version
	collection := m.database.Collection(tableName)
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "version", Value: 1}},
		Options: options.Index().SetUnique(true),
	}

	_, err = collection.Indexes().CreateOne(ctx, indexModel)
	return err
}

// CreateLockTable creates the migration lock collection
func (m *MongoDBBackend) CreateLockTable(ctx context.Context, tableName string) error {
	// Check if collection exists first
	exists, err := m.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Create collection with validation
	validator := bson.M{
		"$jsonSchema": bson.M{
			"bsonType": "object",
			"required": []string{"lock_key"},
			"properties": bson.M{
				"lock_key": bson.M{
					"bsonType":    "string",
					"description": "Lock key",
				},
				"locked_by": bson.M{
					"bsonType":    "string",
					"description": "User who acquired the lock",
				},
				"locked_at": bson.M{
					"bsonType":    "date",
					"description": "Lock acquisition timestamp",
				},
				"expires_at": bson.M{
					"bsonType":    "date",
					"description": "Lock expiration timestamp",
				},
			},
		},
	}

	opts := options.CreateCollection().SetValidator(validator)
	err = m.database.CreateCollection(ctx, tableName, opts)
	if err != nil {
		return err
	}

	// Create unique index on lock_key
	collection := m.database.Collection(tableName)
	indexModel := mongo.IndexModel{
		Keys:    bson.D{{Key: "lock_key", Value: 1}},
		Options: options.Index().SetUnique(true),
	}

	_, err = collection.Indexes().CreateOne(ctx, indexModel)
	return err
}

// CreateBackup creates a database backup
func (m *MongoDBBackend) CreateBackup(ctx context.Context, backupConfig BackupConfig) error {
	// MongoDB backup would typically use mongodump
	return fmt.Errorf("MongoDB backup requires external tools like mongodump")
}

// RestoreBackup restores a database backup
func (m *MongoDBBackend) RestoreBackup(ctx context.Context, restoreConfig RestoreConfig) error {
	// MongoDB restore would typically use mongorestore
	return fmt.Errorf("MongoDB restore requires external tools like mongorestore")
}

// Helper methods

func (m *MongoDBBackend) buildURI(config types.DatabaseConfig) string {
	if config.Host == "" {
		return "mongodb://localhost:27017"
	}

	var uri strings.Builder
	uri.WriteString("mongodb://")

	if config.Username != "" {
		uri.WriteString(config.Username)
		if config.Password != "" {
			uri.WriteString(":")
			uri.WriteString(config.Password)
		}
		uri.WriteString("@")
	}

	uri.WriteString(config.Host)
	if config.Port > 0 {
		uri.WriteString(fmt.Sprintf(":%d", config.Port))
	}

	if config.Database != "" {
		uri.WriteString("/")
		uri.WriteString(config.Database)
	}

	return uri.String()
}

func (m *MongoDBBackend) buildValidator(constraints []ConstraintDefinition) bson.M {
	// Build MongoDB validation rules from constraints
	validator := bson.M{
		"$jsonSchema": bson.M{
			"bsonType":   "object",
			"properties": bson.M{},
		},
	}

	// This is a simplified implementation
	// In practice, you'd need to convert SQL constraints to MongoDB validation rules

	return validator
}

func (m *MongoDBBackend) buildConstraintValidator(constraint ConstraintDefinition) bson.M {
	// Convert SQL constraint to MongoDB validation rule
	switch constraint.Type {
	case ConstraintCheck:
		// Convert check expression to MongoDB validation
		return bson.M{
			"$jsonSchema": bson.M{
				"bsonType": "object",
				// Add validation based on check expression
			},
		}
	default:
		return bson.M{}
	}
}

// MongoDB operation types
type MongoOperation struct {
	Type       string
	Collection string
	Filter     bson.M
	Update     bson.M
	Document   bson.M
	Options    interface{}
}

type MongoOperationResult struct {
	InsertedCount int64
	ModifiedCount int64
	DeletedCount  int64
	UpsertedCount int64
}

func (m *MongoDBBackend) parseOperation(query string, args ...interface{}) (*MongoOperation, error) {
	// This is a simplified parser - in practice, you'd need a full SQL to MongoDB converter
	query = strings.TrimSpace(strings.ToUpper(query))

	if strings.HasPrefix(query, "INSERT") {
		return &MongoOperation{
			Type:       "insert",
			Collection: "migrations", // default collection
			Document:   bson.M{},
		}, nil
	}

	if strings.HasPrefix(query, "SELECT") {
		return &MongoOperation{
			Type:       "find",
			Collection: "migrations",
			Filter:     bson.M{},
		}, nil
	}

	if strings.HasPrefix(query, "UPDATE") {
		return &MongoOperation{
			Type:       "update",
			Collection: "migrations",
			Filter:     bson.M{},
			Update:     bson.M{},
		}, nil
	}

	if strings.HasPrefix(query, "DELETE") {
		return &MongoOperation{
			Type:       "delete",
			Collection: "migrations",
			Filter:     bson.M{},
		}, nil
	}

	return nil, fmt.Errorf("unsupported operation: %s", query)
}

func (m *MongoDBBackend) executeOperation(ctx context.Context, op *MongoOperation) (*MongoOperationResult, error) {
	collection := m.database.Collection(op.Collection)
	result := &MongoOperationResult{}

	switch op.Type {
	case "insert":
		res, err := collection.InsertOne(ctx, op.Document)
		if err != nil {
			return nil, err
		}
		result.InsertedCount = 1
		_ = res // Use the result if needed

	case "update":
		res, err := collection.UpdateMany(ctx, op.Filter, op.Update)
		if err != nil {
			return nil, err
		}
		result.ModifiedCount = res.ModifiedCount
		result.UpsertedCount = res.UpsertedCount

	case "delete":
		res, err := collection.DeleteMany(ctx, op.Filter)
		if err != nil {
			return nil, err
		}
		result.DeletedCount = res.DeletedCount
	}

	return result, nil
}

func (m *MongoDBBackend) executeQuery(ctx context.Context, op *MongoOperation) (*mongo.Cursor, error) {
	collection := m.database.Collection(op.Collection)

	switch op.Type {
	case "find":
		return collection.Find(ctx, op.Filter)
	default:
		return nil, fmt.Errorf("unsupported query operation: %s", op.Type)
	}
}

// MongoDB-specific implementations

// MongoDBTransaction implements Transaction for MongoDB
type MongoDBTransaction struct {
	session  mongo.Session
	database *mongo.Database
	ctx      context.Context
}

func (t *MongoDBTransaction) Execute(ctx context.Context, query string, args ...interface{}) (QueryResult, error) {
	// Execute within session context
	var result *MongoOperationResult
	var err error

	callback := func(sessCtx mongo.SessionContext) (interface{}, error) {
		// Parse and execute operation within transaction
		// This is simplified - you'd need proper SQL to MongoDB conversion
		return &MongoOperationResult{}, nil
	}

	_, err = t.session.WithTransaction(ctx, callback)
	if err != nil {
		return nil, err
	}

	return &MongoDBQueryResult{
		insertedCount: result.InsertedCount,
		modifiedCount: result.ModifiedCount,
		deletedCount:  result.DeletedCount,
	}, nil
}

func (t *MongoDBTransaction) Query(ctx context.Context, query string, args ...interface{}) (QueryRows, error) {
	// Execute query within session context
	return nil, fmt.Errorf("query within transaction not implemented for MongoDB")
}

func (t *MongoDBTransaction) QueryRow(ctx context.Context, query string, args ...interface{}) QueryRow {
	return &MongoDBQueryRow{err: fmt.Errorf("query row within transaction not implemented for MongoDB")}
}

func (t *MongoDBTransaction) Commit() error {
	return t.session.CommitTransaction(t.ctx)
}

func (t *MongoDBTransaction) Rollback() error {
	return t.session.AbortTransaction(t.ctx)
}

func (t *MongoDBTransaction) Savepoint(name string) error {
	// MongoDB doesn't support savepoints
	return fmt.Errorf("MongoDB doesn't support savepoints")
}

func (t *MongoDBTransaction) RollbackToSavepoint(name string) error {
	return fmt.Errorf("MongoDB doesn't support savepoints")
}

func (t *MongoDBTransaction) ReleaseSavepoint(name string) error {
	return fmt.Errorf("MongoDB doesn't support savepoints")
}

// MongoDBQueryResult implements QueryResult for MongoDB
type MongoDBQueryResult struct {
	insertedCount int64
	modifiedCount int64
	deletedCount  int64
}

func (r *MongoDBQueryResult) RowsAffected() (int64, error) {
	return r.modifiedCount + r.insertedCount + r.deletedCount, nil
}

func (r *MongoDBQueryResult) LastInsertId() (int64, error) {
	// MongoDB uses ObjectIDs, not auto-increment integers
	return 0, fmt.Errorf("MongoDB doesn't support auto-increment IDs")
}

// MongoDBQueryRows implements QueryRows for MongoDB
type MongoDBQueryRows struct {
	cursor  *mongo.Cursor
	ctx     context.Context
	current bson.M
}

func (r *MongoDBQueryRows) Next() bool {
	if r.cursor.Next(r.ctx) {
		r.cursor.Decode(&r.current)
		return true
	}
	return false
}

func (r *MongoDBQueryRows) Scan(dest ...interface{}) error {
	// Convert BSON document to SQL-like row scan
	// This is a simplified implementation
	if len(dest) == 0 {
		return nil
	}

	// For now, just convert the first field
	for _, value := range r.current {
		if len(dest) > 0 {
			switch v := dest[0].(type) {
			case *string:
				*v = fmt.Sprintf("%v", value)
			case *int:
				if i, ok := value.(int32); ok {
					*v = int(i)
				}
			case *int64:
				if i, ok := value.(int64); ok {
					*v = i
				}
			}
		}
		break
	}

	return nil
}

func (r *MongoDBQueryRows) Columns() ([]string, error) {
	var columns []string
	for key := range r.current {
		columns = append(columns, key)
	}
	return columns, nil
}

func (r *MongoDBQueryRows) ColumnTypes() ([]*sql.ColumnType, error) {
	// MongoDB doesn't have fixed column types
	return nil, fmt.Errorf("MongoDB doesn't support column types")
}

func (r *MongoDBQueryRows) Close() error {
	return r.cursor.Close(r.ctx)
}

func (r *MongoDBQueryRows) Err() error {
	return r.cursor.Err()
}

// MongoDBQueryRow implements QueryRow for MongoDB
type MongoDBQueryRow struct {
	result *mongo.SingleResult
	err    error
}

func (r *MongoDBQueryRow) Scan(dest ...interface{}) error {
	if r.err != nil {
		return r.err
	}

	var doc bson.M
	if err := r.result.Decode(&doc); err != nil {
		return err
	}

	// Convert BSON document to SQL-like row scan
	// This is a simplified implementation
	if len(dest) == 0 {
		return nil
	}

	// For now, just convert the first field
	for _, value := range doc {
		if len(dest) > 0 {
			switch v := dest[0].(type) {
			case *string:
				*v = fmt.Sprintf("%v", value)
			case *int:
				if i, ok := value.(int32); ok {
					*v = int(i)
				}
			case *int64:
				if i, ok := value.(int64); ok {
					*v = i
				}
			}
		}
		break
	}

	return nil
}

// MongoDBBackendFactory creates MongoDB backends
type MongoDBBackendFactory struct{}

func NewMongoDBBackendFactory() BackendFactory {
	return &MongoDBBackendFactory{}
}

func (f *MongoDBBackendFactory) CreateBackend(dialect DatabaseDialect, config types.DatabaseConfig) (DatabaseBackend, error) {
	if dialect != DialectMongoDB {
		return nil, fmt.Errorf("unsupported dialect: %s", dialect)
	}

	return NewMongoDBBackend(config)
}

func (f *MongoDBBackendFactory) SupportedDialects() []DatabaseDialect {
	return []DatabaseDialect{DialectMongoDB}
}

func (f *MongoDBBackendFactory) GetDialectInfo(dialect DatabaseDialect) DialectInfo {
	if dialect == DialectMongoDB {
		info, _ := GetDialectInfo(dialect)
		return info
	}
	return DialectInfo{}
}

// Register MongoDB backend
func init() {
	RegisterBackend(DialectMongoDB, NewMongoDBBackendFactory())
}
