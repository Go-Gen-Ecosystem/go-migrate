/**
 * @file index_manager.go
 * @description MongoDB index management and optimization
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/index_manager.go
 */

package mongodb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CollectionInterface defines the interface for collection operations
type CollectionInterface interface {
	Indexes() IndexViewInterface
	Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (CursorInterface, error)
	Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (CursorInterface, error)
	UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error)
	DeleteMany(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)
	DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)
	CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error)
	Name() string
	Drop(ctx context.Context) error
}

// IndexViewInterface defines the interface for index operations
// CursorInterface defines the interface for cursor operations
type CursorInterface interface {
	Next(ctx context.Context) bool
	Decode(v interface{}) error
	Close(ctx context.Context) error
	All(ctx context.Context, results interface{}) error
}

type IndexViewInterface interface {
	CreateOne(ctx context.Context, model mongo.IndexModel, opts ...*options.CreateIndexesOptions) (string, error)
	CreateMany(ctx context.Context, models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error)
	DropOne(ctx context.Context, name string, opts ...*options.DropIndexesOptions) (bson.Raw, error)
	List(ctx context.Context, opts ...*options.ListIndexesOptions) (CursorInterface, error)
	ListSpecifications(ctx context.Context, opts ...*options.ListIndexesOptions) ([]*mongo.IndexSpecification, error)
}

// DatabaseInterface defines the interface for MongoDB database operations
type DatabaseInterface interface {
	Collection(name string, opts ...*options.CollectionOptions) CollectionInterface
	RunCommand(ctx context.Context, runCommand interface{}, opts ...*options.RunCmdOptions) *mongo.SingleResult
	CreateCollection(ctx context.Context, name string, opts ...*options.CreateCollectionOptions) error
	Name() string
}

// DatabaseWrapper wraps mongo.Database to implement DatabaseInterface
type DatabaseWrapper struct {
	db *mongo.Database
}

func NewDatabaseWrapper(db *mongo.Database) DatabaseInterface {
	return &DatabaseWrapper{db: db}
}

func (dw *DatabaseWrapper) Collection(name string, opts ...*options.CollectionOptions) CollectionInterface {
	return &CollectionWrapper{collection: dw.db.Collection(name, opts...)}
}

func (dw *DatabaseWrapper) RunCommand(ctx context.Context, runCommand interface{}, opts ...*options.RunCmdOptions) *mongo.SingleResult {
	return dw.db.RunCommand(ctx, runCommand, opts...)
}

func (dw *DatabaseWrapper) CreateCollection(ctx context.Context, name string, opts ...*options.CreateCollectionOptions) error {
	return dw.db.CreateCollection(ctx, name, opts...)
}

func (dw *DatabaseWrapper) Name() string {
	return dw.db.Name()
}

// CollectionWrapper wraps mongo.Collection to implement CollectionInterface
type CollectionWrapper struct {
	collection *mongo.Collection
}

func (cw *CollectionWrapper) Indexes() IndexViewInterface {
	indexView := cw.collection.Indexes()
	return &IndexViewWrapper{indexView: indexView}
}

func (cw *CollectionWrapper) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (CursorInterface, error) {
	return cw.collection.Aggregate(ctx, pipeline, opts...)
}

func (cw *CollectionWrapper) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (CursorInterface, error) {
	return cw.collection.Find(ctx, filter, opts...)
}

func (cw *CollectionWrapper) UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return cw.collection.UpdateMany(ctx, filter, update, opts...)
}

func (cw *CollectionWrapper) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return cw.collection.UpdateOne(ctx, filter, update, opts...)
}

func (cw *CollectionWrapper) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	return cw.collection.InsertOne(ctx, document, opts...)
}

func (cw *CollectionWrapper) DeleteMany(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return cw.collection.DeleteMany(ctx, filter, opts...)
}

func (cw *CollectionWrapper) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	return cw.collection.DeleteOne(ctx, filter, opts...)
}

func (cw *CollectionWrapper) CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	return cw.collection.CountDocuments(ctx, filter, opts...)
}

func (cw *CollectionWrapper) Name() string {
	return cw.collection.Name()
}

func (cw *CollectionWrapper) Drop(ctx context.Context) error {
	return cw.collection.Drop(ctx)
}

// IndexViewWrapper wraps mongo.IndexView to implement IndexViewInterface
type IndexViewWrapper struct {
	indexView mongo.IndexView
}

func (ivw *IndexViewWrapper) CreateOne(ctx context.Context, model mongo.IndexModel, opts ...*options.CreateIndexesOptions) (string, error) {
	return ivw.indexView.CreateOne(ctx, model, opts...)
}

func (ivw *IndexViewWrapper) CreateMany(ctx context.Context, models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error) {
	return ivw.indexView.CreateMany(ctx, models, opts...)
}

func (ivw *IndexViewWrapper) DropOne(ctx context.Context, name string, opts ...*options.DropIndexesOptions) (bson.Raw, error) {
	return ivw.indexView.DropOne(ctx, name, opts...)
}

func (ivw *IndexViewWrapper) List(ctx context.Context, opts ...*options.ListIndexesOptions) (CursorInterface, error) {
	return ivw.indexView.List(ctx, opts...)
}

func (ivw *IndexViewWrapper) ListSpecifications(ctx context.Context, opts ...*options.ListIndexesOptions) ([]*mongo.IndexSpecification, error) {
	return ivw.indexView.ListSpecifications(ctx, opts...)
}

// IndexManager handles MongoDB index operations
type IndexManager struct {
	database DatabaseInterface
	client   *mongo.Client
}

// NewIndexManager creates a new IndexManager
func NewIndexManager(client *mongo.Client, database interface{}) (*IndexManager, error) {
	var db DatabaseInterface
	
	switch d := database.(type) {
	case *mongo.Database:
		db = NewDatabaseWrapper(d)
	case DatabaseInterface:
		db = d
	default:
		return nil, fmt.Errorf("unsupported database type: %T, must be either *mongo.Database or DatabaseInterface", database)
	}
	
	return &IndexManager{
		client:   client,
		database: db,
	}, nil
}

// IndexDefinition represents a complete index definition
type IndexDefinition struct {
	Name           string                 `json:"name"`
	Collection     string                 `json:"collection"`
	Keys           map[string]interface{} `json:"keys"`
	Options        *IndexCreateOptions    `json:"options,omitempty"`
	Background     bool                   `json:"background"`
	DropDuplicates bool                   `json:"drop_duplicates"`
	Sparse         bool                   `json:"sparse"`
	Unique         bool                   `json:"unique"`
	TTL            *time.Duration         `json:"ttl,omitempty"`
	PartialFilter  map[string]interface{} `json:"partial_filter,omitempty"`
	Collation      *CollationOptions      `json:"collation,omitempty"`
	Weights        map[string]int32       `json:"weights,omitempty"`
	Language       string                 `json:"language,omitempty"`
	TextVersion    *int32                 `json:"text_version,omitempty"`
	SphereVersion  *int32                 `json:"sphere_version,omitempty"`
	Bits           *int32                 `json:"bits,omitempty"`
	Min            *float64               `json:"min,omitempty"`
	Max            *float64               `json:"max,omitempty"`
	BucketSize     *int32                 `json:"bucket_size,omitempty"`
}

// ToIndexModel converts IndexDefinition to mongo.IndexModel
func (id *IndexDefinition) ToIndexModel() mongo.IndexModel {
	model := mongo.IndexModel{
		Keys: id.Keys,
	}

	if id.Options != nil {
		opts := options.Index()

		if id.Options.Name != "" {
			opts.SetName(id.Options.Name)
		} else if id.Name != "" {
			opts.SetName(id.Name)
		}

		if id.Options.Unique {
			opts.SetUnique(id.Options.Unique)
		}

		if id.Options.Sparse {
			opts.SetSparse(id.Options.Sparse)
		}

		if id.Options.Background {
			opts.SetBackground(id.Options.Background)
		}

		if id.Options.ExpireAfterSecs != nil {
			opts.SetExpireAfterSeconds(*id.Options.ExpireAfterSecs)
		}

		if id.Options.PartialFilter != nil {
			opts.SetPartialFilterExpression(id.Options.PartialFilter)
		}

		if id.Options.Weights != nil {
			opts.SetWeights(id.Options.Weights)
		}

		if id.Options.DefaultLanguage != "" {
			opts.SetDefaultLanguage(id.Options.DefaultLanguage)
		}

		if id.Options.LanguageOverride != "" {
			opts.SetLanguageOverride(id.Options.LanguageOverride)
		}

		if id.Options.TextVersion != nil {
			opts.SetTextVersion(*id.Options.TextVersion)
		}

		if id.Options.SphereVersion != nil {
			opts.SetSphereVersion(*id.Options.SphereVersion)
		}

		if id.Options.Bits != nil {
			opts.SetBits(*id.Options.Bits)
		}

		if id.Options.Min != nil {
			opts.SetMin(*id.Options.Min)
		}

		if id.Options.Max != nil {
			opts.SetMax(*id.Options.Max)
		}

		if id.Options.BucketSize != nil {
			opts.SetBucketSize(*id.Options.BucketSize)
		}

		if id.Options.Version != nil {
			opts.SetVersion(*id.Options.Version)
		}

		if id.Options.StorageEngine != nil {
			opts.SetStorageEngine(id.Options.StorageEngine)
		}

		if id.Options.Collation != nil {
			collation := &options.Collation{
				Locale:          id.Options.Collation.Locale,
				CaseLevel:       id.Options.Collation.CaseLevel,
				CaseFirst:       id.Options.Collation.CaseFirst,
				Strength:        id.Options.Collation.Strength,
				NumericOrdering: id.Options.Collation.NumericOrdering,
				Alternate:       id.Options.Collation.Alternate,
				MaxVariable:     id.Options.Collation.MaxVariable,
				Backwards:       id.Options.Collation.Backwards,
			}
			opts.SetCollation(collation)
		}

		model.Options = opts
	}

	return model
}

// IndexCreateOptions represents options for creating indexes
type IndexCreateOptions struct {
	Background       bool                   `json:"background"`
	ExpireAfterSecs  *int32                 `json:"expire_after_secs,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Sparse           bool                   `json:"sparse"`
	StorageEngine    map[string]interface{} `json:"storage_engine,omitempty"`
	Unique           bool                   `json:"unique"`
	Version          *int32                 `json:"version,omitempty"`
	DefaultLanguage  string                 `json:"default_language,omitempty"`
	LanguageOverride string                 `json:"language_override,omitempty"`
	TextVersion      *int32                 `json:"text_version,omitempty"`
	Weights          map[string]int32       `json:"weights,omitempty"`
	SphereVersion    *int32                 `json:"sphere_version,omitempty"`
	Bits             *int32                 `json:"bits,omitempty"`
	Min              *float64               `json:"min,omitempty"`
	Max              *float64               `json:"max,omitempty"`
	BucketSize       *int32                 `json:"bucket_size,omitempty"`
	PartialFilter    map[string]interface{} `json:"partial_filter,omitempty"`
	Collation        *CollationOptions      `json:"collation,omitempty"`
}

// CollationOptions represents MongoDB collation options
type CollationOptions struct {
	Locale          string `json:"locale"`
	CaseLevel       bool   `json:"case_level"`
	CaseFirst       string `json:"case_first"`
	Strength        int    `json:"strength"`
	NumericOrdering bool   `json:"numeric_ordering"`
	Alternate       string `json:"alternate"`
	MaxVariable     string `json:"max_variable"`
	Backwards       bool   `json:"backwards"`
}

// IndexOperation represents an index operation
type IndexOperation struct {
	Type       string          `json:"type"`       // create, drop, recreate, modify
	Collection string          `json:"collection"`
	Index      IndexDefinition `json:"index"`
	OldName    string          `json:"old_name,omitempty"` // For rename operations
}

// IndexAnalysis represents analysis of existing indexes
type IndexAnalysis struct {
	Collection     string                 `json:"collection"`
	TotalIndexes   int                    `json:"total_indexes"`
	TotalSize      int64                  `json:"total_size_bytes"`
	UnusedIndexes  []string               `json:"unused_indexes"`
	DuplicateIndexes [][]string           `json:"duplicate_indexes"`
	Recommendations []IndexRecommendation `json:"recommendations"`
	Performance    IndexPerformanceStats  `json:"performance"`
}

// IndexRecommendation represents an index optimization recommendation
type IndexRecommendation struct {
	Type        string `json:"type"`        // create, drop, modify, compound
	Collection  string `json:"collection"`
	IndexName   string `json:"index_name,omitempty"`
	Reason      string `json:"reason"`
	Impact      string `json:"impact"`      // high, medium, low
	Suggestion  string `json:"suggestion"`
	Fields      []string `json:"fields,omitempty"`
}

// IndexUsageInfo represents information about index usage
type IndexUsageInfo struct {
	IndexName     string    `json:"index_name"`
	Collection    string    `json:"collection"`
	UsageCount    int64     `json:"usage_count"`
	LastUsed      time.Time `json:"last_used"`
	AvgQueryTime  float64   `json:"avg_query_time_ms"`
	KeyPattern    bson.D    `json:"key_pattern"`
}

// IndexPerformanceStats represents index performance statistics
type IndexPerformanceStats struct {
	QueriesAnalyzed    int                        `json:"queries_analyzed"`
	IndexHitRatio      float64                    `json:"index_hit_ratio"`
	SlowQueries        []SlowQueryInfo            `json:"slow_queries"`
	MostUsedIndexes    []IndexUsageInfo           `json:"most_used_indexes"`
	LeastUsedIndexes   []IndexUsageInfo           `json:"least_used_indexes"`
	IndexEfficiency    map[string]float64         `json:"index_efficiency"`
}

// SlowQueryInfo represents information about slow queries
type SlowQueryInfo struct {
	Query           map[string]interface{} `json:"query"`
	ExecutionTime   time.Duration          `json:"execution_time"`
	DocsExamined    int64                  `json:"docs_examined"`
	DocsReturned    int64                  `json:"docs_returned"`
	IndexesUsed     []string               `json:"indexes_used"`
	Recommendation  string                 `json:"recommendation"`
}

// IndexUsage represents index usage statistics
type IndexUsage struct {
	Name       string `bson:"name"`
	Accesses   int64  `bson:"accesses"`
	Since      string `bson:"since"`
	Collection string `bson:"collection"`
}

// GetIndexUsage retrieves index usage statistics for a collection
func (im *IndexManager) GetIndexUsage(ctx context.Context, collectionName string) ([]IndexUsage, error) {
	collection := im.database.Collection(collectionName)
	
	// MongoDB aggregation pipeline to get index usage statistics
	pipeline := []bson.M{
		{
			"$indexStats": bson.M{},
		},
		{
			"$project": bson.M{
				"name":       "$name",
				"accesses":   "$accesses.ops",
				"since":      "$accesses.since",
				"collection": collectionName,
			},
		},
	}
	
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to get index usage: %w", err)
	}
	defer cursor.Close(ctx)
	
	var usage []IndexUsage
	if err := cursor.All(ctx, &usage); err != nil {
		return nil, fmt.Errorf("failed to decode index usage: %w", err)
	}
	
	return usage, nil
}

// CreateIndexes creates multiple indexes
func (im *IndexManager) CreateIndexes(ctx context.Context, operations []IndexOperation) error {
	for _, op := range operations {
		if op.Type != "create" {
			continue
		}

		if err := im.CreateIndex(ctx, op.Collection, op.Index); err != nil {
			return fmt.Errorf("failed to create index %s on %s: %w", op.Index.Name, op.Collection, err)
		}
	}
	return nil
}

// ExecuteOperations executes mixed create and drop operations
func (im *IndexManager) ExecuteOperations(ctx context.Context, operations []IndexOperation) error {
	for _, op := range operations {
		switch op.Type {
		case "create":
			if err := im.CreateIndex(ctx, op.Collection, op.Index); err != nil {
				return fmt.Errorf("failed to create index %s on %s: %w", op.Index.Name, op.Collection, err)
			}
		case "drop":
			if err := im.DropIndex(ctx, op.Collection, op.Index.Name); err != nil {
				return fmt.Errorf("failed to drop index %s from %s: %w", op.Index.Name, op.Collection, err)
			}
		default:
			return fmt.Errorf("unsupported operation type: %s", op.Type)
		}
	}
	return nil
}

// CreateIndex creates a single index
func (im *IndexManager) CreateIndex(ctx context.Context, collectionName string, indexDef IndexDefinition) error {
	collection := im.database.Collection(collectionName)

	// Build index model
	indexModel := mongo.IndexModel{
		Keys: indexDef.Keys,
	}

	// Set index options
	opts := options.Index()
	
	if indexDef.Name != "" {
		opts.SetName(indexDef.Name)
	}
	
	if indexDef.Unique {
		opts.SetUnique(true)
	}
	
	if indexDef.Sparse {
		opts.SetSparse(true)
	}
	
	if indexDef.Background {
		opts.SetBackground(true)
	}
	
	if indexDef.TTL != nil {
		opts.SetExpireAfterSeconds(int32(indexDef.TTL.Seconds()))
	}
	
	if indexDef.PartialFilter != nil {
		opts.SetPartialFilterExpression(indexDef.PartialFilter)
	}
	
	if indexDef.Collation != nil {
		collation := &options.Collation{
			Locale:          indexDef.Collation.Locale,
			CaseLevel:       indexDef.Collation.CaseLevel,
			CaseFirst:       indexDef.Collation.CaseFirst,
			Strength:        indexDef.Collation.Strength,
			NumericOrdering: indexDef.Collation.NumericOrdering,
			Alternate:       indexDef.Collation.Alternate,
			MaxVariable:     indexDef.Collation.MaxVariable,
			Backwards:       indexDef.Collation.Backwards,
		}
		opts.SetCollation(collation)
	}
	
	if indexDef.Weights != nil {
		opts.SetWeights(indexDef.Weights)
	}
	
	if indexDef.Language != "" {
		opts.SetDefaultLanguage(indexDef.Language)
	}
	
	if indexDef.TextVersion != nil {
		opts.SetTextVersion(*indexDef.TextVersion)
	}
	
	if indexDef.SphereVersion != nil {
		opts.SetSphereVersion(*indexDef.SphereVersion)
	}
	
	if indexDef.Bits != nil {
		opts.SetBits(*indexDef.Bits)
	}
	
	if indexDef.Min != nil {
		opts.SetMin(*indexDef.Min)
	}
	
	if indexDef.Max != nil {
		opts.SetMax(*indexDef.Max)
	}
	
	if indexDef.BucketSize != nil {
		opts.SetBucketSize(*indexDef.BucketSize)
	}

	indexModel.Options = opts

	// Create the index
	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	fmt.Printf("Created index %s on collection %s\n", indexDef.Name, collectionName)
	return nil
}

// DropIndex drops an index
func (im *IndexManager) DropIndex(ctx context.Context, collectionName, indexName string) error {
	collection := im.database.Collection(collectionName)
	
	_, err := collection.Indexes().DropOne(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to drop index %s: %w", indexName, err)
	}

	fmt.Printf("Dropped index %s from collection %s\n", indexName, collectionName)
	return nil
}

// DropIndexes drops multiple indexes based on operations
func (im *IndexManager) DropIndexes(ctx context.Context, operations []IndexOperation) error {
	for _, op := range operations {
		if op.Type != "drop" {
			continue
		}
		
		err := im.DropIndex(ctx, op.Collection, op.Index.Name)
		if err != nil {
			return fmt.Errorf("failed to drop index %s from collection %s: %w", op.Index.Name, op.Collection, err)
		}
	}
	return nil
}

// ListIndexes lists all indexes for a collection
func (im *IndexManager) ListIndexes(ctx context.Context, collectionName string) ([]IndexDefinition, error) {
	collection := im.database.Collection(collectionName)
	
	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexes: %w", err)
	}
	defer cursor.Close(ctx)

	var indexes []IndexDefinition
	for cursor.Next(ctx) {
		var indexDoc bson.M
		if err := cursor.Decode(&indexDoc); err != nil {
			continue
		}

		indexDef := im.parseIndexDocument(indexDoc, collectionName)
		indexes = append(indexes, indexDef)
	}

	return indexes, nil
}

// parseIndexDocument converts a MongoDB index document to IndexDefinition
func (im *IndexManager) parseIndexDocument(doc bson.M, collectionName string) IndexDefinition {
	indexDef := IndexDefinition{
		Collection: collectionName,
	}

	if name, ok := doc["name"].(string); ok {
		indexDef.Name = name
	}

	if keys, ok := doc["key"].(bson.M); ok {
		indexDef.Keys = keys
	}

	if unique, ok := doc["unique"].(bool); ok {
		indexDef.Unique = unique
	}

	if sparse, ok := doc["sparse"].(bool); ok {
		indexDef.Sparse = sparse
	}

	if background, ok := doc["background"].(bool); ok {
		indexDef.Background = background
	}

	if expireAfterSeconds, ok := doc["expireAfterSeconds"].(int32); ok {
		ttl := time.Duration(expireAfterSeconds) * time.Second
		indexDef.TTL = &ttl
	}

	if partialFilterExpression, ok := doc["partialFilterExpression"].(bson.M); ok {
		indexDef.PartialFilter = partialFilterExpression
	}

	if weights, ok := doc["weights"].(bson.M); ok {
		indexDef.Weights = make(map[string]int32)
		for k, v := range weights {
			if weight, ok := v.(int32); ok {
				indexDef.Weights[k] = weight
			}
		}
	}

	if defaultLanguage, ok := doc["default_language"].(string); ok {
		indexDef.Language = defaultLanguage
	}

	if textIndexVersion, ok := doc["textIndexVersion"].(int32); ok {
		indexDef.TextVersion = &textIndexVersion
	}

	if sphereIndexVersion, ok := doc["2dsphereIndexVersion"].(int32); ok {
		indexDef.SphereVersion = &sphereIndexVersion
	}

	return indexDef
}

// AnalyzeIndexes performs comprehensive index analysis
func (im *IndexManager) AnalyzeIndexes(ctx context.Context, collectionName string) (*IndexAnalysis, error) {
	// Get all indexes
	indexes, err := im.ListIndexes(ctx, collectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to list indexes: %w", err)
	}

	// Get index statistics
	stats, err := im.getIndexStats(ctx, collectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get index stats: %w", err)
	}

	// Analyze for unused indexes
	unusedIndexes := im.findUnusedIndexes(stats)

	// Analyze for duplicate indexes
	duplicateIndexes := im.findDuplicateIndexes(indexes)

	// Generate recommendations
	recommendations := im.generateRecommendations(ctx, collectionName, indexes, stats)

	// Calculate performance metrics
	performance := im.calculatePerformanceMetrics(stats)

	analysis := &IndexAnalysis{
		Collection:       collectionName,
		TotalIndexes:     len(indexes),
		TotalSize:        im.calculateTotalIndexSize(stats),
		UnusedIndexes:    unusedIndexes,
		DuplicateIndexes: duplicateIndexes,
		Recommendations:  recommendations,
		Performance:      performance,
	}

	return analysis, nil
}

// getIndexStats retrieves index usage statistics
func (im *IndexManager) getIndexStats(ctx context.Context, collectionName string) (map[string]interface{}, error) {
	pipeline := []bson.M{
		{
			"$indexStats": bson.M{},
		},
	}

	collection := im.database.Collection(collectionName)
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	stats := make(map[string]interface{})
	for cursor.Next(ctx) {
		var stat bson.M
		if err := cursor.Decode(&stat); err != nil {
			continue
		}

		if name, ok := stat["name"].(string); ok {
			stats[name] = stat
		}
	}

	return stats, nil
}

// findUnusedIndexes identifies indexes that haven't been used
func (im *IndexManager) findUnusedIndexes(stats map[string]interface{}) []string {
	var unused []string

	for indexName, statData := range stats {
		if stat, ok := statData.(bson.M); ok {
			if accesses, ok := stat["accesses"].(bson.M); ok {
				if ops, ok := accesses["ops"].(int64); ok && ops == 0 {
					// Skip the default _id index
					if indexName != "_id_" {
						unused = append(unused, indexName)
					}
				}
			}
		}
	}

	return unused
}

// findDuplicateIndexes identifies duplicate or redundant indexes
func (im *IndexManager) findDuplicateIndexes(indexes []IndexDefinition) [][]string {
	var duplicates [][]string
	indexGroups := make(map[string][]string)

	// Group indexes by their key signature
	for _, index := range indexes {
		signature := im.getIndexSignature(index.Keys)
		indexGroups[signature] = append(indexGroups[signature], index.Name)
	}

	// Find groups with multiple indexes
	for _, group := range indexGroups {
		if len(group) > 1 {
			duplicates = append(duplicates, group)
		}
	}

	return duplicates
}

// getIndexSignature creates a signature for an index based on its keys
func (im *IndexManager) getIndexSignature(keys map[string]interface{}) string {
	var parts []string
	for field, direction := range keys {
		parts = append(parts, fmt.Sprintf("%s:%v", field, direction))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// generateRecommendations generates optimization recommendations
func (im *IndexManager) generateRecommendations(ctx context.Context, collectionName string, indexes []IndexDefinition, stats map[string]interface{}) []IndexRecommendation {
	var recommendations []IndexRecommendation

	// Recommend dropping unused indexes
	unusedIndexes := im.findUnusedIndexes(stats)
	for _, indexName := range unusedIndexes {
		recommendations = append(recommendations, IndexRecommendation{
			Type:       "drop",
			Collection: collectionName,
			IndexName:  indexName,
			Reason:     "Index is not being used",
			Impact:     "medium",
			Suggestion: fmt.Sprintf("Consider dropping unused index %s to save storage space", indexName),
		})
	}

	// Recommend consolidating duplicate indexes
	duplicates := im.findDuplicateIndexes(indexes)
	for _, group := range duplicates {
		if len(group) > 1 {
			recommendations = append(recommendations, IndexRecommendation{
				Type:       "modify",
				Collection: collectionName,
				Reason:     "Duplicate indexes detected",
				Impact:     "high",
				Suggestion: fmt.Sprintf("Consolidate duplicate indexes: %s", strings.Join(group, ", ")),
			})
		}
	}

	// Recommend compound indexes for common query patterns
	// This would require query log analysis in a real implementation
	recommendations = append(recommendations, IndexRecommendation{
		Type:       "create",
		Collection: collectionName,
		Reason:     "Potential compound index opportunity",
		Impact:     "medium",
		Suggestion: "Analyze query patterns to identify compound index opportunities",
	})

	return recommendations
}

// calculatePerformanceMetrics calculates index performance metrics
func (im *IndexManager) calculatePerformanceMetrics(stats map[string]interface{}) IndexPerformanceStats {
	performance := IndexPerformanceStats{
		IndexEfficiency: make(map[string]float64),
	}

	totalOps := int64(0)
	totalIndexOps := int64(0)

	for indexName, statData := range stats {
		if stat, ok := statData.(bson.M); ok {
			if accesses, ok := stat["accesses"].(bson.M); ok {
				if ops, ok := accesses["ops"].(int64); ok {
					totalOps += ops
					if indexName != "_id_" {
						totalIndexOps += ops
					}

					// Calculate efficiency (simplified metric)
					efficiency := float64(ops) / (float64(ops) + 1.0) // Avoid division by zero
					performance.IndexEfficiency[indexName] = efficiency
				}
			}
		}
	}

	if totalOps > 0 {
		performance.IndexHitRatio = float64(totalIndexOps) / float64(totalOps)
	}

	return performance
}

// calculateTotalIndexSize calculates the total size of all indexes
func (im *IndexManager) calculateTotalIndexSize(stats map[string]interface{}) int64 {
	totalSize := int64(0)

	for _, statData := range stats {
		if stat, ok := statData.(bson.M); ok {
			if size, ok := stat["size"].(int64); ok {
				totalSize += size
			}
		}
	}

	return totalSize
}

// OptimizeIndexes applies optimization recommendations
func (im *IndexManager) OptimizeIndexes(ctx context.Context, collectionName string, recommendations []IndexRecommendation) error {
	for _, rec := range recommendations {
		switch rec.Type {
		case "drop":
			if rec.IndexName != "" {
				if err := im.DropIndex(ctx, collectionName, rec.IndexName); err != nil {
					fmt.Printf("Warning: Failed to drop index %s: %v\n", rec.IndexName, err)
				}
			}
		case "create":
			// Would need more specific index definition
			fmt.Printf("Recommendation: %s\n", rec.Suggestion)
		case "modify":
			fmt.Printf("Manual action required: %s\n", rec.Suggestion)
		}
	}

	return nil
}

// RebuildIndexes rebuilds all indexes for a collection
func (im *IndexManager) RebuildIndexes(ctx context.Context, collectionName string) error {
	command := bson.D{
		{Key: "reIndex", Value: collectionName},
	}

	err := im.database.RunCommand(ctx, command).Err()
	if err != nil {
		return fmt.Errorf("failed to rebuild indexes: %w", err)
	}

	fmt.Printf("Rebuilt all indexes for collection %s\n", collectionName)
	return nil
}

// ValidateIndexDefinition validates an index definition
func (im *IndexManager) ValidateIndexDefinition(indexDef IndexDefinition) error {
	if indexDef.Name == "" {
		return fmt.Errorf("index name is required")
	}
	
	if len(indexDef.Keys) == 0 {
		return fmt.Errorf("index keys are required")
	}

	// Validate key values
	for field, direction := range indexDef.Keys {
		if field == "" {
			return fmt.Errorf("field name cannot be empty")
		}

		switch v := direction.(type) {
		case int, int32, int64:
			if v != 1 && v != -1 {
				return fmt.Errorf("index direction must be 1 or -1, got %v for field %s", v, field)
			}
		case string:
			validTypes := []string{"text", "2d", "2dsphere", "hashed"}
			valid := false
			for _, validType := range validTypes {
				if v == validType {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid index type %s for field %s", v, field)
			}
		default:
			return fmt.Errorf("invalid index direction type for field %s", field)
		}
	}

	// Validate TTL
	if indexDef.TTL != nil && *indexDef.TTL < 0 {
		return fmt.Errorf("TTL cannot be negative")
	}

	return nil
}

// ValidateOperations validates a slice of index operations
func (im *IndexManager) ValidateOperations(operations []IndexOperation) error {
	for i, op := range operations {
		// Validate operation type
		switch op.Type {
		case "create", "drop", "recreate", "modify":
			// Valid operation types
		default:
			return fmt.Errorf("invalid operation type '%s' at index %d", op.Type, i)
		}

		// Validate collection name
		if op.Collection == "" {
			return fmt.Errorf("collection name is required for operation at index %d", i)
		}

		// Validate index definition for create operations
		if op.Type == "create" || op.Type == "recreate" || op.Type == "modify" {
			if err := im.ValidateIndexDefinition(op.Index); err != nil {
				return fmt.Errorf("invalid index definition for operation at index %d: %w", i, err)
			}
		}

		// Validate index name for drop operations
		if op.Type == "drop" && op.Index.Name == "" {
			return fmt.Errorf("index name is required for drop operation at index %d", i)
		}
	}
	return nil
}

// GetIndexRecommendations provides index recommendations for a collection
func (im *IndexManager) GetIndexRecommendations(ctx context.Context, collectionName string) ([]IndexRecommendation, error) {
	analysis, err := im.AnalyzeIndexes(ctx, collectionName)
	if err != nil {
		return nil, err
	}

	return analysis.Recommendations, nil
}

// SlowQuery represents a slow query from the profiler
type SlowQuery struct {
	Timestamp   time.Time              `bson:"ts"`
	Command     string                 `bson:"command"`
	Collection  string                 `bson:"ns"`
	Duration    int64                  `bson:"millis"`
	QueryFilter map[string]interface{} `bson:"command.filter,omitempty"`
}

// GetSlowQueries retrieves slow queries from the system.profile collection
func (im *IndexManager) GetSlowQueries(ctx context.Context, duration time.Duration) ([]SlowQuery, error) {
	profileCollection := im.database.Collection("system.profile")
	
	// Query for slow queries within the specified duration
	since := time.Now().Add(-duration)
	filter := bson.M{
		"ts": bson.M{"$gte": since},
		"millis": bson.M{"$gte": 100}, // Only queries taking more than 100ms
	}
	
	cursor, err := profileCollection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query slow queries: %w", err)
	}
	defer cursor.Close(ctx)
	
	var slowQueries []SlowQuery
	if err := cursor.All(ctx, &slowQueries); err != nil {
		return nil, fmt.Errorf("failed to decode slow queries: %w", err)
	}
	
	return slowQueries, nil
}

// CreateMultipleIndexes creates multiple indexes on a collection
func (im *IndexManager) CreateMultipleIndexes(ctx context.Context, collectionName string, definitions []IndexDefinition) error {
	collection := im.database.Collection(collectionName)
	
	// Convert IndexDefinition to mongo.IndexModel
	var models []mongo.IndexModel
	for _, def := range definitions {
		model := mongo.IndexModel{
			Keys: def.Keys,
		}
		
		if def.Options != nil {
			opts := &options.IndexOptions{}
			if def.Options.Unique {
				opts.SetUnique(true)
			}
			if def.Options.Background {
				opts.SetBackground(true)
			}
			if def.Options.Sparse {
				opts.SetSparse(true)
			}
			if def.Options.ExpireAfterSecs != nil {
				opts.SetExpireAfterSeconds(*def.Options.ExpireAfterSecs)
			}
			if def.Name != "" {
				opts.SetName(def.Name)
			}
			model.Options = opts
		}
		
		models = append(models, model)
	}
	
	// Create the indexes
	_, err := collection.Indexes().CreateMany(ctx, models)
	if err != nil {
		return fmt.Errorf("failed to create multiple indexes: %w", err)
	}
	
	return nil
}