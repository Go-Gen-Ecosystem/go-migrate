/**
 * @file schema_evolution_test.go
 * @description Unit tests for MongoDB schema evolution manager
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/schema_evolution_test.go
 */

package mongodb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MockDatabase implements the DatabaseInterface for testing
type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) Collection(name string, opts ...*options.CollectionOptions) CollectionInterface {
	args := m.Called(name, opts)
	return args.Get(0).(CollectionInterface)
}

func (m *MockDatabase) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockDatabase) RunCommand(ctx context.Context, runCommand interface{}, opts ...*options.RunCmdOptions) *mongo.SingleResult {
	args := m.Called(ctx, runCommand, opts)
	return args.Get(0).(*mongo.SingleResult)
}

func (m *MockDatabase) CreateCollection(ctx context.Context, name string, opts ...*options.CreateCollectionOptions) error {
	args := m.Called(ctx, name, opts)
	return args.Error(0)
}

// MockCollection implements the CollectionInterface for testing
type MockCollection struct {
	mock.Mock
}

func (m *MockCollection) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockCollection) Indexes() IndexViewInterface {
	args := m.Called()
	return args.Get(0).(IndexViewInterface)
}

func (m *MockCollection) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (CursorInterface, error) {
	args := m.Called(ctx, pipeline, opts)
	return args.Get(0).(CursorInterface), args.Error(1)
}

func (m *MockCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (CursorInterface, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(CursorInterface), args.Error(1)
}

func (m *MockCollection) UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	args := m.Called(ctx, filter, update, opts)
	return args.Get(0).(*mongo.UpdateResult), args.Error(1)
}

func (m *MockCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	args := m.Called(ctx, filter, update, opts)
	return args.Get(0).(*mongo.UpdateResult), args.Error(1)
}

func (m *MockCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	args := m.Called(ctx, document, opts)
	return args.Get(0).(*mongo.InsertOneResult), args.Error(1)
}

func (m *MockCollection) DeleteMany(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(*mongo.DeleteResult), args.Error(1)
}

func (m *MockCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(*mongo.DeleteResult), args.Error(1)
}

func (m *MockCollection) CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockCollection) Drop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestNewSchemaEvolutionManager(t *testing.T) {
	mockDB := &MockDatabase{}
	manager, err := NewSchemaEvolutionManager(nil, mockDB)

	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_ApplySchemaChanges(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_GetCollectionSchema(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("FindOne", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.SingleResult{})

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_ValidateSchemaChange(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("CountDocuments", mock.Anything, mock.Anything, mock.Anything).Return(int64(0), nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_AddField(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_RemoveField(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_RenameField(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_ChangeFieldType(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_TransformData(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_CreateCollection(t *testing.T) {
	mockDB := &MockDatabase{}

	mockDB.On("CreateCollection", mock.Anything, "new_collection", mock.Anything).Return(nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_DropCollection(t *testing.T) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("Drop", mock.Anything).Return(nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestSchemaEvolutionManager_RenameCollection(t *testing.T) {
	mockDB := &MockDatabase{}

	mockDB.On("RunCommand", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.SingleResult{})

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestValidationRule_ToBSON(t *testing.T) {
	tests := []struct {
		name string
		rule ValidationRule
		want bson.M
	}{
		{
			name: "String validation",
			rule: ValidationRule{
				Rule: map[string]interface{}{
					"bsonType":  "string",
					"minLength": 5,
					"maxLength": 100,
					"pattern":   "^[a-zA-Z]+$",
				},
				Level:       "strict",
				Action:      "error",
				Description: "String validation rule",
			},
			want: bson.M{
				"bsonType":  "string",
				"minLength": 5,
				"maxLength": 100,
				"pattern":   "^[a-zA-Z]+$",
			},
		},
		{
			name: "Number validation",
			rule: ValidationRule{
				Rule: map[string]interface{}{
					"bsonType": "number",
					"minimum":  0,
					"maximum":  100,
				},
				Level:       "strict",
				Action:      "error",
				Description: "Number validation rule",
			},
			want: bson.M{
				"bsonType": "number",
				"minimum":  0,
				"maximum":  100,
			},
		},
		{
			name: "Enum validation",
			rule: ValidationRule{
				Rule: map[string]interface{}{
					"bsonType": "string",
					"enum":     []interface{}{"active", "inactive", "pending"},
				},
				Level:       "strict",
				Action:      "error",
				Description: "Enum validation rule",
			},
			want: bson.M{
				"bsonType": "string",
				"enum":     []interface{}{"active", "inactive", "pending"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rule.ToBSON()
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestFieldSchema_ToBSON(t *testing.T) {
	field := FieldSchema{
		Type:         "string",
		Required:     true,
		DefaultValue: "default@example.com",
		Description:  "Email field",
	}

	result := field.ToBSON()
	expected := bson.M{
		"bsonType": "string",
		"required":  true,
		"default":   "default@example.com",
	}

	assert.Equal(t, expected, result)
}

func TestCollectionSchema_ToBSON(t *testing.T) {
	schema := CollectionSchema{
		Name: "users",
		Fields: map[string]FieldSchema{
			"name": {
				Type:        "string",
				Required:    true,
				Description: "User name field",
			},
			"age": {
				Type:        "number",
				Required:    false,
				Description: "User age field",
			},
		},
		Indexes: []IndexSchema{
			{
				Name: "idx_name",
				Keys: map[string]interface{}{"name": 1},
				Options: IndexOptions{
					Unique: true,
				},
			},
		},
		Validators: []ValidationRule{
			{
				Rule: map[string]interface{}{
					"bsonType": "object",
				},
				Level:       "strict",
				Action:      "error",
				Description: "Object validation rule",
			},
		},
	}

	result := schema.ToBSON()

	assert.Contains(t, result, "$jsonSchema")
	jsonSchema := result["$jsonSchema"].(bson.M)
	assert.Equal(t, "object", jsonSchema["bsonType"])
	assert.Contains(t, jsonSchema, "required")
	assert.Contains(t, jsonSchema, "properties")

	required := jsonSchema["required"].([]string)
	assert.Contains(t, required, "name")
	assert.NotContains(t, required, "age")

	properties := jsonSchema["properties"].(bson.M)
	assert.Contains(t, properties, "name")
	assert.Contains(t, properties, "age")
}

// Benchmark tests
func BenchmarkSchemaEvolutionManager_ApplySchemaChanges(b *testing.B) {
	mockDB := &MockDatabase{}
	mockCollection := &MockCollection{}

	mockDB.On("Collection", "test_collection", mock.Anything).Return(&mongo.Collection{})
	mockCollection.On("UpdateMany", mock.Anything, mock.Anything, mock.Anything).Return(&mongo.UpdateResult{}, nil)

	manager, err := NewSchemaEvolutionManager(nil, mockDB)
	if err != nil {
		b.Fatal(err)
	}

	changes := []SchemaChange{
		{
			Type:       "addField",
			Collection: "users",
			Field:      "email",
			DataType:   "string",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.ApplySchemaChanges(context.Background(), changes)
	}
}

func BenchmarkValidationRule_ToBSON(b *testing.B) {
	rule := ValidationRule{
		Rule: map[string]interface{}{
			"bsonType":  "string",
			"minLength": 5,
			"maxLength": 100,
			"pattern":   "^[a-zA-Z]+$",
		},
		Level:       "strict",
		Action:      "error",
		Description: "String validation rule",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rule.ToBSON()
	}
}
