/**
 * @file schema_evolution.go
 * @description MongoDB schema evolution and collection management
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/schema_evolution.go
 */

package mongodb

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SchemaEvolutionManager handles MongoDB schema evolution operations
type SchemaEvolutionManager struct {
	database DatabaseInterface
	client   *mongo.Client
}

// NewSchemaEvolutionManager creates a new schema evolution manager
func NewSchemaEvolutionManager(client *mongo.Client, database interface{}) (*SchemaEvolutionManager, error) {
	var db DatabaseInterface
	
	switch d := database.(type) {
	case *mongo.Database:
		db = NewDatabaseWrapper(d)
	case DatabaseInterface:
		db = d
	default:
		// For backward compatibility, try to cast to *mongo.Database
		if mongoDb, ok := database.(*mongo.Database); ok {
			db = NewDatabaseWrapper(mongoDb)
		} else {
			return nil, fmt.Errorf("unsupported database type: %T, must be either *mongo.Database or DatabaseInterface", database)
		}
	}
	
	return &SchemaEvolutionManager{
		client:   client,
		database: db,
	}, nil
}

// SchemaChange represents a schema evolution operation
type SchemaChange struct {
	Type           string                 `json:"type"`           // add_field, remove_field, rename_field, change_type, etc.
	Collection     string                 `json:"collection"`
	Field          string                 `json:"field,omitempty"`
	NewField       string                 `json:"new_field,omitempty"`
	DataType       string                 `json:"data_type,omitempty"`
	DefaultValue   interface{}            `json:"default_value,omitempty"`
	Transformation string                 `json:"transformation,omitempty"` // JavaScript transformation code
	Options        map[string]interface{} `json:"options,omitempty"`
	Condition      string                 `json:"condition,omitempty"` // Optional condition for selective updates
}

// CollectionSchema represents the schema of a MongoDB collection
type CollectionSchema struct {
	Name        string                 `json:"name"`
	Fields      map[string]FieldSchema `json:"fields"`
	Indexes     []IndexSchema          `json:"indexes"`
	Validators  []ValidationRule       `json:"validators"`
	Options     CollectionOptions      `json:"options"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Version     string                 `json:"version"`
}

// FieldSchema represents a field in a MongoDB document
type FieldSchema struct {
	Type         string      `json:"type"`          // string, number, boolean, date, object, array
	Required     bool        `json:"required"`
	DefaultValue interface{} `json:"default_value,omitempty"`
	Validation   string      `json:"validation,omitempty"` // JSON Schema validation
	Description  string      `json:"description,omitempty"`
	Deprecated   bool        `json:"deprecated"`
	AddedIn      string      `json:"added_in,omitempty"`    // Version when field was added
	RemovedIn    string      `json:"removed_in,omitempty"`  // Version when field was removed
}

// IndexSchema represents an index on a MongoDB collection
type IndexSchema struct {
	Name       string                 `json:"name"`
	Keys       map[string]interface{} `json:"keys"`       // Field names and sort order
	Options    IndexOptions           `json:"options"`
	CreatedAt  time.Time              `json:"created_at"`
	Version    string                 `json:"version"`
}

// IndexOptions represents MongoDB index options
type IndexOptions struct {
	Unique          bool                   `json:"unique"`
	Sparse          bool                   `json:"sparse"`
	Background      bool                   `json:"background"`
	ExpireAfterSecs *int32                 `json:"expire_after_secs,omitempty"`
	PartialFilter   map[string]interface{} `json:"partial_filter,omitempty"`
	Collation       *options.Collation     `json:"collation,omitempty"`
	TextOptions     *TextIndexOptions      `json:"text_options,omitempty"`
}

// TextIndexOptions represents text index specific options
type TextIndexOptions struct {
	DefaultLanguage string            `json:"default_language,omitempty"`
	LanguageOverride string           `json:"language_override,omitempty"`
	Weights         map[string]int32  `json:"weights,omitempty"`
}

// ValidationRule represents a document validation rule
type ValidationRule struct {
	Rule        map[string]interface{} `json:"rule"`        // JSON Schema validation rule
	Level       string                 `json:"level"`       // strict, moderate
	Action      string                 `json:"action"`      // error, warn
	Description string                 `json:"description"`
}

// CollectionOptions represents MongoDB collection options
type CollectionOptions struct {
	Capped          bool   `json:"capped"`
	Size            int64  `json:"size,omitempty"`
	MaxDocuments    int64  `json:"max_documents,omitempty"`
	ValidationLevel string `json:"validation_level,omitempty"` // strict, moderate, off
	ValidationAction string `json:"validation_action,omitempty"` // error, warn
}

// ApplySchemaChanges applies a list of schema changes to collections
func (sem *SchemaEvolutionManager) ApplySchemaChanges(ctx context.Context, changes []SchemaChange) error {
	session, err := sem.client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	return mongo.WithSession(ctx, session, func(sc mongo.SessionContext) error {
		for i, change := range changes {
			if err := sem.applySchemaChange(sc, change); err != nil {
				return fmt.Errorf("failed to apply schema change %d: %w", i+1, err)
			}
		}
		return nil
	})
}

// applySchemaChange applies a single schema change
func (sem *SchemaEvolutionManager) applySchemaChange(ctx context.Context, change SchemaChange) error {
	collection := sem.database.Collection(change.Collection)

	switch change.Type {
	case "add_field":
		return sem.addField(ctx, collection, change)
	case "remove_field":
		return sem.removeField(ctx, collection, change)
	case "rename_field":
		return sem.renameField(ctx, collection, change)
	case "change_type":
		return sem.changeFieldType(ctx, collection, change)
	case "add_validation":
		return sem.addValidation(ctx, collection, change)
	case "remove_validation":
		return sem.removeValidation(ctx, collection, change)
	case "transform_data":
		return sem.transformData(ctx, collection, change)
	case "create_collection":
		return sem.createCollection(ctx, change)
	case "drop_collection":
		return sem.dropCollection(ctx, change)
	case "rename_collection":
		return sem.renameCollection(ctx, change)
	default:
		return fmt.Errorf("unsupported schema change type: %s", change.Type)
	}
}

// addField adds a new field to documents in a collection
func (sem *SchemaEvolutionManager) addField(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	filter := bson.M{}
	if change.Condition != "" {
		// Parse condition (simplified - in production, use a proper parser)
		filter = bson.M{"$expr": bson.M{"$eq": bson.A{change.Condition, true}}}
	}

	// Add field that doesn't exist
	filter[change.Field] = bson.M{"$exists": false}

	update := bson.M{
		"$set": bson.M{
			change.Field: change.DefaultValue,
		},
	}

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to add field %s: %w", change.Field, err)
	}

	fmt.Printf("Added field %s to %d documents\n", change.Field, result.ModifiedCount)
	return nil
}

// ToBSON converts ValidationRule to BSON format
func (vr ValidationRule) ToBSON() bson.M {
	return vr.Rule
}

// ToBSON converts FieldSchema to BSON format
func (fs FieldSchema) ToBSON() bson.M {
	result := bson.M{
		"bsonType": fs.Type,
	}
	
	if fs.Required {
		result["required"] = true
	}
	
	if fs.DefaultValue != nil {
		result["default"] = fs.DefaultValue
	}
	
	return result
}

// ToBSON converts CollectionSchema to BSON format
func (cs CollectionSchema) ToBSON() bson.M {
	properties := bson.M{}
	required := []string{}
	
	for fieldName, field := range cs.Fields {
		properties[fieldName] = field.ToBSON()
		if field.Required {
			required = append(required, fieldName)
		}
	}
	
	jsonSchema := bson.M{
		"bsonType":   "object",
		"properties": properties,
	}
	
	if len(required) > 0 {
		jsonSchema["required"] = required
	}
	
	return bson.M{
		"$jsonSchema": jsonSchema,
	}
}

// removeField removes a field from documents in a collection
func (sem *SchemaEvolutionManager) removeField(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	filter := bson.M{}
	if change.Condition != "" {
		filter = bson.M{"$expr": bson.M{"$eq": bson.A{change.Condition, true}}}
	}

	// Only remove field if it exists
	filter[change.Field] = bson.M{"$exists": true}

	update := bson.M{
		"$unset": bson.M{
			change.Field: "",
		},
	}

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to remove field %s: %w", change.Field, err)
	}

	fmt.Printf("Removed field %s from %d documents\n", change.Field, result.ModifiedCount)
	return nil
}

// renameField renames a field in documents
func (sem *SchemaEvolutionManager) renameField(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	filter := bson.M{}
	if change.Condition != "" {
		filter = bson.M{"$expr": bson.M{"$eq": bson.A{change.Condition, true}}}
	}

	// Only rename if old field exists and new field doesn't exist
	filter[change.Field] = bson.M{"$exists": true}
	filter[change.NewField] = bson.M{"$exists": false}

	update := bson.M{
		"$rename": bson.M{
			change.Field: change.NewField,
		},
	}

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to rename field %s to %s: %w", change.Field, change.NewField, err)
	}

	fmt.Printf("Renamed field %s to %s in %d documents\n", change.Field, change.NewField, result.ModifiedCount)
	return nil
}

// changeFieldType changes the data type of a field
func (sem *SchemaEvolutionManager) changeFieldType(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	if change.Transformation == "" {
		return fmt.Errorf("transformation script is required for type change")
	}

	// Use aggregation pipeline to transform field type
	pipeline := []bson.M{
		{
			"$addFields": bson.M{
				change.Field: bson.M{
					"$switch": bson.M{
						"branches": []bson.M{
							{
								"case": bson.M{"$type": "$" + change.Field},
								"then": bson.M{"$function": bson.M{
									"body": change.Transformation,
									"args": []string{"$" + change.Field},
									"lang": "js",
								}},
							},
						},
						"default": "$" + change.Field,
					},
				},
			},
		},
		{
			"$merge": bson.M{
				"into": collection.Name(),
				"whenMatched": "replace",
			},
		},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("failed to change field type for %s: %w", change.Field, err)
	}
	defer cursor.Close(ctx)

	fmt.Printf("Changed type of field %s to %s\n", change.Field, change.DataType)
	return nil
}

// transformData applies custom data transformation
func (sem *SchemaEvolutionManager) transformData(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	if change.Transformation == "" {
		return fmt.Errorf("transformation script is required")
	}

	// Execute transformation using MongoDB's $function operator
	pipeline := []bson.M{
		{
			"$addFields": bson.M{
				"__transformed": bson.M{
					"$function": bson.M{
						"body": change.Transformation,
						"args": []string{"$$ROOT"},
						"lang": "js",
					},
				},
			},
		},
		{
			"$replaceRoot": bson.M{
				"newRoot": "$__transformed",
			},
		},
		{
			"$merge": bson.M{
				"into": collection.Name(),
				"whenMatched": "replace",
			},
		},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("failed to transform data: %w", err)
	}
	defer cursor.Close(ctx)

	fmt.Printf("Applied data transformation to collection %s\n", change.Collection)
	return nil
}

// addValidation adds document validation to a collection
func (sem *SchemaEvolutionManager) addValidation(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	if change.Options == nil {
		return fmt.Errorf("validation options are required")
	}

	validator, ok := change.Options["validator"]
	if !ok {
		return fmt.Errorf("validator is required in options")
	}

	validationLevel := "strict"
	if level, ok := change.Options["validationLevel"].(string); ok {
		validationLevel = level
	}

	validationAction := "error"
	if action, ok := change.Options["validationAction"].(string); ok {
		validationAction = action
	}

	command := bson.D{
		{Key: "collMod", Value: collection.Name()},
		{Key: "validator", Value: validator},
		{Key: "validationLevel", Value: validationLevel},
		{Key: "validationAction", Value: validationAction},
	}

	err := sem.database.RunCommand(ctx, command).Err()
	if err != nil {
		return fmt.Errorf("failed to add validation: %w", err)
	}

	fmt.Printf("Added validation to collection %s\n", change.Collection)
	return nil
}

// removeValidation removes document validation from a collection
func (sem *SchemaEvolutionManager) removeValidation(ctx context.Context, collection CollectionInterface, change SchemaChange) error {
	command := bson.D{
		{Key: "collMod", Value: collection.Name()},
		{Key: "validator", Value: bson.M{}},
		{Key: "validationLevel", Value: "off"},
	}

	err := sem.database.RunCommand(ctx, command).Err()
	if err != nil {
		return fmt.Errorf("failed to remove validation: %w", err)
	}

	fmt.Printf("Removed validation from collection %s\n", change.Collection)
	return nil
}

// createCollection creates a new collection with options
func (sem *SchemaEvolutionManager) createCollection(ctx context.Context, change SchemaChange) error {
	opts := options.CreateCollection()

	if change.Options != nil {
		if capped, ok := change.Options["capped"].(bool); ok && capped {
			opts.SetCapped(true)
			if size, ok := change.Options["size"].(int64); ok {
				opts.SetSizeInBytes(size)
			}
			if maxDocs, ok := change.Options["maxDocuments"].(int64); ok {
				opts.SetMaxDocuments(maxDocs)
			}
		}

		if validator, ok := change.Options["validator"]; ok {
			opts.SetValidator(validator)
		}

		if level, ok := change.Options["validationLevel"].(string); ok {
			opts.SetValidationLevel(level)
		}

		if action, ok := change.Options["validationAction"].(string); ok {
			opts.SetValidationAction(action)
		}
	}

	err := sem.database.CreateCollection(ctx, change.Collection, opts)
	if err != nil {
		return fmt.Errorf("failed to create collection %s: %w", change.Collection, err)
	}

	fmt.Printf("Created collection %s\n", change.Collection)
	return nil
}

// dropCollection drops a collection
func (sem *SchemaEvolutionManager) dropCollection(ctx context.Context, change SchemaChange) error {
	collection := sem.database.Collection(change.Collection)
	err := collection.Drop(ctx)
	if err != nil {
		return fmt.Errorf("failed to drop collection %s: %w", change.Collection, err)
	}

	fmt.Printf("Dropped collection %s\n", change.Collection)
	return nil
}

// renameCollection renames a collection
func (sem *SchemaEvolutionManager) renameCollection(ctx context.Context, change SchemaChange) error {
	if change.NewField == "" {
		return fmt.Errorf("new collection name is required")
	}

	command := bson.D{
		{Key: "renameCollection", Value: fmt.Sprintf("%s.%s", sem.database.Name(), change.Collection)},
		{Key: "to", Value: fmt.Sprintf("%s.%s", sem.database.Name(), change.NewField)},
	}

	err := sem.client.Database("admin").RunCommand(ctx, command).Err()
	if err != nil {
		return fmt.Errorf("failed to rename collection %s to %s: %w", change.Collection, change.NewField, err)
	}

	fmt.Printf("Renamed collection %s to %s\n", change.Collection, change.NewField)
	return nil
}

// GetCollectionSchema retrieves the current schema of a collection
func (sem *SchemaEvolutionManager) GetCollectionSchema(ctx context.Context, collectionName string) (*CollectionSchema, error) {
	collection := sem.database.Collection(collectionName)

	// Get collection info
	var collInfo bson.M
	err := sem.database.RunCommand(ctx, bson.D{
		{Key: "listCollections", Value: 1},
		{Key: "filter", Value: bson.M{"name": collectionName}},
	}).Decode(&collInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}

	// Analyze documents to infer schema
	fields, err := sem.inferSchemaFromDocuments(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("failed to infer schema: %w", err)
	}

	// Get indexes
	indexes, err := sem.getCollectionIndexes(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("failed to get indexes: %w", err)
	}

	schema := &CollectionSchema{
		Name:      collectionName,
		Fields:    fields,
		Indexes:   indexes,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Version:   "1.0.0",
	}

	return schema, nil
}

// inferSchemaFromDocuments analyzes documents to infer field types
func (sem *SchemaEvolutionManager) inferSchemaFromDocuments(ctx context.Context, collection CollectionInterface) (map[string]FieldSchema, error) {
	fields := make(map[string]FieldSchema)

	// Sample documents to analyze schema
	cursor, err := collection.Find(ctx, bson.M{}, options.Find().SetLimit(1000))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	fieldTypes := make(map[string]map[string]int) // field -> type -> count

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		sem.analyzeDocument(doc, "", fieldTypes)
	}

	// Convert analysis to schema
	for fieldName, types := range fieldTypes {
		mostCommonType := ""
		maxCount := 0
		for typeName, count := range types {
			if count > maxCount {
				maxCount = count
				mostCommonType = typeName
			}
		}

		fields[fieldName] = FieldSchema{
			Type:        mostCommonType,
			Required:    false, // Would need more analysis to determine
			Description: fmt.Sprintf("Inferred from %d samples", maxCount),
		}
	}

	return fields, nil
}

// analyzeDocument recursively analyzes a document to extract field types
func (sem *SchemaEvolutionManager) analyzeDocument(doc bson.M, prefix string, fieldTypes map[string]map[string]int) {
	for key, value := range doc {
		fieldName := key
		if prefix != "" {
			fieldName = prefix + "." + key
		}

		if fieldTypes[fieldName] == nil {
			fieldTypes[fieldName] = make(map[string]int)
		}

		typeName := sem.getValueType(value)
		fieldTypes[fieldName][typeName]++

		// Recursively analyze nested objects
		if nestedDoc, ok := value.(bson.M); ok {
			sem.analyzeDocument(nestedDoc, fieldName, fieldTypes)
		}
	}
}

// getValueType determines the type of a BSON value
func (sem *SchemaEvolutionManager) getValueType(value interface{}) string {
	switch v := value.(type) {
	case string:
		return "string"
	case int, int32, int64:
		return "number"
	case float32, float64:
		return "number"
	case bool:
		return "boolean"
	case time.Time, primitive.DateTime:
		return "date"
	case primitive.ObjectID:
		return "objectId"
	case []interface{}:
		return "array"
	case bson.M, map[string]interface{}:
		return "object"
	case nil:
		return "null"
	default:
		return reflect.TypeOf(v).String()
	}
}

// getCollectionIndexes retrieves all indexes for a collection
func (sem *SchemaEvolutionManager) getCollectionIndexes(ctx context.Context, collection CollectionInterface) ([]IndexSchema, error) {
	var indexes []IndexSchema

	cursor, err := collection.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var indexDoc bson.M
		if err := cursor.Decode(&indexDoc); err != nil {
			continue
		}

		name, _ := indexDoc["name"].(string)
		keys, _ := indexDoc["key"].(bson.M)

		indexSchema := IndexSchema{
			Name:      name,
			Keys:      keys,
			CreatedAt: time.Now(),
			Version:   "1.0.0",
		}

		// Parse index options
		if unique, ok := indexDoc["unique"].(bool); ok {
			indexSchema.Options.Unique = unique
		}
		if sparse, ok := indexDoc["sparse"].(bool); ok {
			indexSchema.Options.Sparse = sparse
		}
		if background, ok := indexDoc["background"].(bool); ok {
			indexSchema.Options.Background = background
		}
		if expireAfterSeconds, ok := indexDoc["expireAfterSeconds"].(int32); ok {
			indexSchema.Options.ExpireAfterSecs = &expireAfterSeconds
		}

		indexes = append(indexes, indexSchema)
	}

	return indexes, nil
}

// ValidateSchemaChange validates a schema change before applying it
func (sem *SchemaEvolutionManager) ValidateSchemaChange(ctx context.Context, change SchemaChange) error {
	if change.Collection == "" {
		return fmt.Errorf("collection name is required")
	}

	switch change.Type {
	case "add_field":
		if change.Field == "" {
			return fmt.Errorf("field name is required for add_field operation")
		}
	case "remove_field":
		if change.Field == "" {
			return fmt.Errorf("field name is required for remove_field operation")
		}
	case "rename_field":
		if change.Field == "" || change.NewField == "" {
			return fmt.Errorf("both field and new_field are required for rename_field operation")
		}
	case "change_type":
		if change.Field == "" || change.DataType == "" || change.Transformation == "" {
			return fmt.Errorf("field, data_type, and transformation are required for change_type operation")
		}
	case "transform_data":
		if change.Transformation == "" {
			return fmt.Errorf("transformation script is required for transform_data operation")
		}
	}

	return nil
}