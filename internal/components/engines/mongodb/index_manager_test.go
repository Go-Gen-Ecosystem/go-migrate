/**
 * @file index_manager_test.go
 * @description Unit tests for MongoDB index manager
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engines/mongodb/index_manager_test.go
 */

package mongodb

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Helper function to create int32 pointer
func int32Ptr(i int32) *int32 {
	return &i
}

// MockIndexDatabase is a mock implementation of DatabaseInterface for IndexManager
type MockIndexDatabase struct {
	mock.Mock
}

func (m *MockIndexDatabase) Collection(name string, opts ...*options.CollectionOptions) CollectionInterface {
	args := m.Called(name, opts)
	return args.Get(0).(CollectionInterface)
}

func (m *MockIndexDatabase) RunCommand(ctx context.Context, runCommand interface{}, opts ...*options.RunCmdOptions) *mongo.SingleResult {
	args := m.Called(ctx, runCommand, opts)
	return args.Get(0).(*mongo.SingleResult)
}

func (m *MockIndexDatabase) CreateCollection(ctx context.Context, name string, opts ...*options.CreateCollectionOptions) error {
	args := m.Called(ctx, name, opts)
	return args.Error(0)
}

func (m *MockIndexDatabase) Name() string {
	args := m.Called()
	return args.String(0)
}

// MockIndexCollection is a mock implementation of CollectionInterface for IndexManager
type MockIndexCollection struct {
	mock.Mock
}

func (m *MockIndexCollection) Indexes() IndexViewInterface {
	args := m.Called()
	return args.Get(0).(IndexViewInterface)
}

func (m *MockIndexCollection) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (CursorInterface, error) {
	args := m.Called(ctx, pipeline, opts)
	return args.Get(0).(CursorInterface), args.Error(1)
}

func (m *MockIndexCollection) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (CursorInterface, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(CursorInterface), args.Error(1)
}

func (m *MockIndexCollection) UpdateMany(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	args := m.Called(ctx, filter, update, opts)
	return args.Get(0).(*mongo.UpdateResult), args.Error(1)
}

func (m *MockIndexCollection) UpdateOne(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	args := m.Called(ctx, filter, update, opts)
	return args.Get(0).(*mongo.UpdateResult), args.Error(1)
}

func (m *MockIndexCollection) InsertOne(ctx context.Context, document interface{}, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	args := m.Called(ctx, document, opts)
	return args.Get(0).(*mongo.InsertOneResult), args.Error(1)
}

func (m *MockIndexCollection) DeleteMany(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(*mongo.DeleteResult), args.Error(1)
}

func (m *MockIndexCollection) DeleteOne(ctx context.Context, filter interface{}, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(*mongo.DeleteResult), args.Error(1)
}

func (m *MockIndexCollection) CountDocuments(ctx context.Context, filter interface{}, opts ...*options.CountOptions) (int64, error) {
	args := m.Called(ctx, filter, opts)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockIndexCollection) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockIndexCollection) Drop(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockIndexView is a mock implementation of IndexViewInterface
type MockIndexView struct {
	mock.Mock
}

func (m *MockIndexView) CreateOne(ctx context.Context, model mongo.IndexModel, opts ...*options.CreateIndexesOptions) (string, error) {
	args := m.Called(ctx, model, opts)
	return args.String(0), args.Error(1)
}

func (m *MockIndexView) DropOne(ctx context.Context, name string, opts ...*options.DropIndexesOptions) (bson.Raw, error) {
	args := m.Called(ctx, name, opts)
	return args.Get(0).(bson.Raw), args.Error(1)
}

func (m *MockIndexView) List(ctx context.Context, opts ...*options.ListIndexesOptions) (CursorInterface, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).(CursorInterface), args.Error(1)
}

func (m *MockIndexView) CreateMany(ctx context.Context, models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error) {
	args := m.Called(ctx, models, opts)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockIndexView) DropAll(ctx context.Context, opts ...*options.DropIndexesOptions) (bson.Raw, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).(bson.Raw), args.Error(1)
}

func (m *MockIndexView) ListSpecifications(ctx context.Context, opts ...*options.ListIndexesOptions) ([]*mongo.IndexSpecification, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).([]*mongo.IndexSpecification), args.Error(1)
}

// MockCursor is a mock implementation of mongo.Cursor for testing
// MockCursor implements CursorInterface for testing
type MockCursor struct {
	documents []bson.M
	current   int
}

// Ensure MockCursor implements CursorInterface
var _ CursorInterface = (*MockCursor)(nil)

func NewMockCursor(documents []bson.M) *MockCursor {
	return &MockCursor{
		documents: documents,
		current:   -1,
	}
}

func (m *MockCursor) Next(ctx context.Context) bool {
	m.current++
	return m.current < len(m.documents)
}

func (m *MockCursor) Decode(v interface{}) error {
	if m.current >= 0 && m.current < len(m.documents) {
		// Simulate decoding by setting the document data
		if doc, ok := v.(*bson.M); ok {
			*doc = m.documents[m.current]
		}
	}
	return nil
}

func (m *MockCursor) Close(ctx context.Context) error {
	return nil
}

func (m *MockCursor) All(ctx context.Context, results interface{}) error {
	// Convert results to a slice pointer
	resultsValue := reflect.ValueOf(results)
	if resultsValue.Kind() != reflect.Ptr || resultsValue.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("results must be a pointer to a slice")
	}
	
	sliceValue := resultsValue.Elem()
	sliceType := sliceValue.Type()
	elementType := sliceType.Elem()
	
	// Create a new slice with the same length as documents
	newSlice := reflect.MakeSlice(sliceType, len(m.documents), len(m.documents))
	
	// Copy each document to the slice
	for i, doc := range m.documents {
		elementValue := newSlice.Index(i)
		
		// Convert bson.M to the target type
		docBytes, err := bson.Marshal(doc)
		if err != nil {
			return err
		}
		
		// Create a new element of the target type
		newElement := reflect.New(elementType).Interface()
		if err := bson.Unmarshal(docBytes, newElement); err != nil {
			return err
		}
		
		elementValue.Set(reflect.ValueOf(newElement).Elem())
	}
	
	// Set the results slice
	sliceValue.Set(newSlice)
	return nil
}

func TestNewIndexManager(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	manager, err := NewIndexManager(nil, mockDB)

	assert.NoError(t, err)
	assert.NotNil(t, manager)
	assert.Equal(t, mockDB, manager.database)
}

func TestIndexManager_CreateIndex(t *testing.T) {
	tests := []struct {
		name        string
		collection  string
		indexDef    IndexDefinition
		expectError bool
	}{
		{
			name:       "Create simple index",
			collection: "users",
			indexDef: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{"email": 1},
			},
			expectError: false,
		},
		{
			name:       "Create unique index",
			collection: "users",
			indexDef: IndexDefinition{
				Name:   "idx_username",
				Keys:   map[string]interface{}{"username": 1},
				Unique: true,
			},
			expectError: false,
		},
		{
			name:       "Create compound index",
			collection: "posts",
			indexDef: IndexDefinition{
				Name: "idx_user_created",
				Keys: map[string]interface{}{"user_id": 1, "created_at": -1},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockDB := &MockIndexDatabase{}
				mockCollection := &MockIndexCollection{}
				mockIndexView := &MockIndexView{}

				mockDB.On("Collection", tt.collection, mock.Anything).Return(mockCollection)
				mockCollection.On("Indexes").Return(mockIndexView)
				mockIndexView.On("CreateOne", mock.Anything, mock.Anything, mock.Anything).Return("", nil)

				manager, err := NewIndexManager(nil, mockDB)
				assert.NoError(t, err)
				err = manager.CreateIndex(context.Background(), tt.collection, tt.indexDef)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockDB.AssertExpectations(t)
			mockCollection.AssertExpectations(t)
			mockIndexView.AssertExpectations(t)
		})
	}
}

func TestIndexManager_CreateIndexes(t *testing.T) {
	operations := []IndexOperation{
		{
			Type:       "create",
			Collection: "users",
			Index: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{"email": 1},
			},
		},
		{
			Type:       "create",
			Collection: "posts",
			Index: IndexDefinition{
				Name: "idx_title",
				Keys: map[string]interface{}{"title": 1},
			},
		},
	}

	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockDB.On("Collection", "posts", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("CreateOne", mock.Anything, mock.Anything, mock.Anything).Return("", nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	err = manager.CreateIndexes(context.Background(), operations)

	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_DropIndexes(t *testing.T) {
	operations := []IndexOperation{
		{
			Type:       "drop",
			Collection: "users",
			Index: IndexDefinition{
				Name: "idx_email",
			},
		},
	}

	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("DropOne", mock.Anything, "idx_email", mock.Anything).Return(bson.Raw{}, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	err = manager.DropIndexes(context.Background(), operations)

	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_ExecuteOperations(t *testing.T) {
	operations := []IndexOperation{
		{
			Type:       "create",
			Collection: "users",
			Index: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{"email": 1},
			},
		},
		{
			Type:       "drop",
			Collection: "users",
			Index: IndexDefinition{
				Name: "idx_old",
			},
		},
	}

	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("CreateOne", mock.Anything, mock.Anything, mock.Anything).Return("", nil)
	mockIndexView.On("DropOne", mock.Anything, "idx_old", mock.Anything).Return(bson.Raw{}, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	err = manager.ExecuteOperations(context.Background(), operations)

	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_ValidateOperations(t *testing.T) {
	tests := []struct {
		name        string
		operations  []IndexOperation
		expectError bool
	}{
		{
			name: "Valid operations",
			operations: []IndexOperation{
				{
					Type:       "create",
					Collection: "users",
					Index: IndexDefinition{
						Name: "idx_email",
						Keys: map[string]interface{}{"email": 1},
					},
				},
			},
			expectError: false,
		},
		{
			name: "Invalid operation type",
			operations: []IndexOperation{
				{
					Type:       "invalid",
					Collection: "users",
					Index: IndexDefinition{
						Name: "idx_email",
						Keys: map[string]interface{}{"email": 1},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockDB := &MockIndexDatabase{}
				manager, err := NewIndexManager(nil, mockDB)
				assert.NoError(t, err)
				err = manager.ValidateOperations(tt.operations)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIndexManager_DropIndex(t *testing.T) {
	tests := []struct {
		name        string
		collection  string
		indexName   string
		expectError bool
	}{
		{
			name:        "Drop existing index",
			collection:  "users",
			indexName:   "idx_email",
			expectError: false,
		},
		{
			name:        "Drop non-existent index",
			collection:  "users",
			indexName:   "idx_nonexistent",
			expectError: false, // MongoDB doesn't error on dropping non-existent indexes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockIndexDatabase{}
			mockCollection := &MockIndexCollection{}
			mockIndexView := &MockIndexView{}

			mockDB.On("Collection", tt.collection, mock.Anything).Return(mockCollection)
			mockCollection.On("Indexes").Return(mockIndexView)
			mockIndexView.On("DropOne", mock.Anything, tt.indexName, mock.Anything).Return(bson.Raw{}, nil)

			manager, err := NewIndexManager(nil, mockDB)
			assert.NoError(t, err)
			err = manager.DropIndex(context.Background(), tt.collection, tt.indexName)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockDB.AssertExpectations(t)
			mockCollection.AssertExpectations(t)
			mockIndexView.AssertExpectations(t)
		})
	}
}

func TestIndexManager_ListIndexes(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	// Create mock index documents
	indexDocs := []bson.M{
		{
			"name": "_id_",
			"key":  bson.M{"_id": 1},
		},
		{
			"name": "idx_email",
			"key":  bson.M{"email": 1},
		},
		{
			"name": "idx_user_status",
			"key":  bson.M{"status": 1, "user_id": 1},
		},
	}

	mockCursor := NewMockCursor(indexDocs)

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("List", mock.Anything, mock.Anything).Return(mockCursor, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	indexes, err := manager.ListIndexes(context.Background(), "users")

	assert.NoError(t, err)
	assert.Len(t, indexes, 3)
	assert.Equal(t, "_id_", indexes[0].Name)
	assert.Equal(t, "idx_email", indexes[1].Name)
	assert.Equal(t, "idx_user_status", indexes[2].Name)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_AnalyzeIndexes(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}





	// Mock the List method for ListIndexes call
	mockCursor := NewMockCursor([]bson.M{
		{
			"name": "_id_",
			"key":  bson.M{"_id": 1},
		},
		{
			"name": "idx_email",
			"key":  bson.M{"email": 1},
		},
	})

	// Mock for getIndexStats aggregation - create a mock cursor with stats data
	statsData := []bson.M{
		{
			"name": "idx_email",
			"accesses": bson.M{
				"ops": int64(100),
				"since": "2024-01-01T00:00:00Z",
			},
		},
	}
	mockStatsCursor := NewMockCursor(statsData)
	
	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockCollection.On("Aggregate", mock.Anything, mock.Anything, mock.Anything).Return(mockStatsCursor, nil)
	mockIndexView.On("List", mock.Anything, mock.Anything).Return(mockCursor, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	analysis, err := manager.AnalyzeIndexes(context.Background(), "users")

	assert.NoError(t, err)
	assert.NotNil(t, analysis)
	assert.Equal(t, "users", analysis.Collection)
	assert.Equal(t, 2, analysis.TotalIndexes)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_OptimizeIndexes(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("DropOne", mock.Anything, "unused_index", mock.Anything).Return(bson.Raw{}, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	
	// Create some sample recommendations to test with
	recommendations := []IndexRecommendation{
		{
			Type:       "drop",
			Collection: "users",
			IndexName:  "unused_index",
			Reason:     "Index is not being used",
			Impact:     "medium",
			Suggestion: "Consider dropping unused index",
		},
	}

	err = manager.OptimizeIndexes(context.Background(), "users", recommendations)

	assert.NoError(t, err)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_RebuildIndexes(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	
	// Create a SingleResult that represents a successful reIndex command
	// The reIndex command typically returns { "ok": 1 }
	result := bson.M{"ok": 1}
	data, _ := bson.Marshal(result)
	successfulResult := mongo.NewSingleResultFromDocument(data, nil, nil)
	
	mockDB.On("RunCommand", mock.Anything, mock.MatchedBy(func(cmd interface{}) bool {
		if cmdDoc, ok := cmd.(bson.D); ok {
			for _, elem := range cmdDoc {
				if elem.Key == "reIndex" && elem.Value == "users" {
					return true
				}
			}
		}
		return false
	}), mock.Anything).Return(successfulResult)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	err = manager.RebuildIndexes(context.Background(), "users")

	assert.NoError(t, err)

	mockDB.AssertExpectations(t)
}

func TestIndexDefinition_ToIndexModel(t *testing.T) {
	tests := []struct {
		name       string
		definition IndexDefinition
		validate   func(t *testing.T, model mongo.IndexModel)
	}{
		{
			name: "Simple index",
			definition: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{"email": 1},
				Options: &IndexCreateOptions{
					Unique:     true,
					Background: true,
				},
			},
			validate: func(t *testing.T, model mongo.IndexModel) {
				// Keys should be converted to bson.D by ToIndexModel
				assert.NotNil(t, model.Keys)
				assert.NotNil(t, model.Options)
				assert.Equal(t, "idx_email", *model.Options.Name)
				assert.True(t, *model.Options.Unique)
				assert.True(t, *model.Options.Background)
			},
		},
		{
			name: "Text index with weights",
			definition: IndexDefinition{
				Name: "idx_text_search",
				Keys: map[string]interface{}{"title": "text", "content": "text"},
				Options: &IndexCreateOptions{
					Background: true,
					Weights: map[string]int32{
						"title":   10,
						"content": 5,
					},
					DefaultLanguage: "english",
				},
			},
			validate: func(t *testing.T, model mongo.IndexModel) {
				// Keys should be converted to bson.D by ToIndexModel
				assert.NotNil(t, model.Keys)
				assert.NotNil(t, model.Options)
				assert.Equal(t, "idx_text_search", *model.Options.Name)
				assert.True(t, *model.Options.Background)
				assert.Equal(t, "english", *model.Options.DefaultLanguage)

				expectedWeights := map[string]int32{
					"title":   int32(10),
					"content": int32(5),
				}
				assert.Equal(t, expectedWeights, model.Options.Weights)
			},
		},
		{
			name: "Sparse index with TTL",
			definition: IndexDefinition{
				Name: "idx_expires",
				Keys: map[string]interface{}{"expires_at": 1},
				Options: &IndexCreateOptions{
					Sparse:          true,
					ExpireAfterSecs: int32Ptr(3600),
					Background:      true,
				},
			},
			validate: func(t *testing.T, model mongo.IndexModel) {
				// Keys should be converted to bson.D by ToIndexModel
				assert.NotNil(t, model.Keys)
				assert.NotNil(t, model.Options)
				assert.Equal(t, "idx_expires", *model.Options.Name)
				assert.True(t, *model.Options.Sparse)
				assert.Equal(t, int32(3600), *model.Options.ExpireAfterSeconds)
				assert.True(t, *model.Options.Background)
			},
		},
		{
			name: "Partial index",
			definition: IndexDefinition{
				Name: "idx_active_users",
				Keys: map[string]interface{}{"created_at": -1},
				Options: &IndexCreateOptions{
					Background: true,
					PartialFilter: map[string]interface{}{
						"status": "active",
					},
				},
			},
			validate: func(t *testing.T, model mongo.IndexModel) {
				// Keys should be converted to bson.D by ToIndexModel
				assert.NotNil(t, model.Keys)
				assert.NotNil(t, model.Options)
				assert.Equal(t, "idx_active_users", *model.Options.Name)
				assert.True(t, *model.Options.Background)

				expectedFilter := map[string]interface{}{
					"status": "active",
				}
				assert.Equal(t, expectedFilter, model.Options.PartialFilterExpression)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := tt.definition.ToIndexModel()
			tt.validate(t, model)
		})
	}
}

func TestIndexManager_CreateMultipleIndexes(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	definitions := []IndexDefinition{
		{
			Name: "idx_email",
			Keys: map[string]interface{}{"email": 1},
			Options: &IndexCreateOptions{
				Unique:     true,
				Background: true,
			},
		},
		{
			Name: "idx_status",
			Keys: map[string]interface{}{"status": 1},
			Options: &IndexCreateOptions{
				Background: true,
			},
		},
	}

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("CreateMany", mock.Anything, mock.Anything, mock.Anything).Return([]string{"idx_email", "idx_status"}, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	err = manager.CreateMultipleIndexes(context.Background(), "users", definitions)

	assert.NoError(t, err)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
	mockIndexView.AssertExpectations(t)
}

func TestIndexManager_GetIndexUsage(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockCursor := NewMockCursor([]bson.M{})

	mockDB.On("Collection", "users", mock.Anything).Return(mockCollection)
	mockCollection.On("Aggregate", mock.Anything, mock.Anything, mock.Anything).Return(mockCursor, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	usage, err := manager.GetIndexUsage(context.Background(), "users")

	assert.NoError(t, err)
	assert.NotNil(t, usage)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
}

func TestIndexManager_GetSlowQueries(t *testing.T) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockCursor := NewMockCursor([]bson.M{})

	// Mock the system.profile collection
	mockDB.On("Collection", "system.profile", mock.Anything).Return(mockCollection)
	mockCollection.On("Find", mock.Anything, mock.Anything, mock.Anything).Return(mockCursor, nil)

	manager, err := NewIndexManager(nil, mockDB)
	assert.NoError(t, err)
	slowQueries, err := manager.GetSlowQueries(context.Background(), time.Hour)

	assert.NoError(t, err)
	assert.NotNil(t, slowQueries)

	mockDB.AssertExpectations(t)
	mockCollection.AssertExpectations(t)
}

func TestIndexManager_ValidateIndexDefinition(t *testing.T) {
	tests := []struct {
		name        string
		definition  IndexDefinition
		expectError bool
	}{
		{
			name: "Valid simple index",
			definition: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{"email": 1},
			},
			expectError: false,
		},
		{
			name: "Invalid - empty name",
			definition: IndexDefinition{
				Name: "",
				Keys: map[string]interface{}{"email": 1},
			},
			expectError: true,
		},
		{
			name: "Invalid - empty fields",
			definition: IndexDefinition{
				Name: "idx_email",
				Keys: map[string]interface{}{},
			},
			expectError: true,
		},
		{
			name: "Invalid - nil fields",
			definition: IndexDefinition{
				Name: "idx_email",
				Keys: nil,
			},
			expectError: true,
		},
		{
			name: "Valid compound index",
			definition: IndexDefinition{
				Name: "idx_user_status",
				Keys: map[string]interface{}{"user_id": 1, "status": 1},
			},
			expectError: false,
		},
		{
			name: "Valid text index",
			definition: IndexDefinition{
				Name: "idx_text",
				Keys: map[string]interface{}{"title": "text", "content": "text"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockDB := &MockIndexDatabase{}
				manager, err := NewIndexManager(nil, mockDB)
				assert.NoError(t, err)

				err = manager.ValidateIndexDefinition(tt.definition)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmark tests
func BenchmarkIndexManager_CreateIndex(b *testing.B) {
	mockDB := &MockIndexDatabase{}
	mockCollection := &MockIndexCollection{}
	mockIndexView := &MockIndexView{}

	mockDB.On("Collection", mock.Anything, mock.Anything).Return(mockCollection)
	mockCollection.On("Indexes").Return(mockIndexView)
	mockIndexView.On("CreateOne", mock.Anything, mock.Anything, mock.Anything).Return("index_created", nil)

	manager, err := NewIndexManager(nil, mockDB)
	if err != nil {
		b.Fatal(err)
	}

	definition := IndexDefinition{
		Name: "idx_email",
		Keys: map[string]interface{}{"email": 1},
		Options: &IndexCreateOptions{
			Unique:     true,
			Background: true,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.CreateIndex(context.Background(), "users", definition)
	}
}

func BenchmarkIndexDefinition_ToIndexModel(b *testing.B) {
	definition := IndexDefinition{
		Name: "idx_compound",
		Keys: map[string]interface{}{"user_id": 1, "status": 1, "created_at": -1},
		Options: &IndexCreateOptions{
			Background: true,
			Unique:     false,
			Sparse:     true,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = definition.ToIndexModel()
	}
}

func BenchmarkIndexManager_ValidateIndexDefinition(b *testing.B) {
	mockDB := &MockIndexDatabase{}
	manager, err := NewIndexManager(nil, mockDB)
	if err != nil {
		b.Fatal(err)
	}

	definition := IndexDefinition{
		Name:   "idx_email",
		Keys: map[string]interface{}{"email": 1},
		Options: &IndexCreateOptions{
			Unique:     true,
			Background: true,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.ValidateIndexDefinition(definition)
	}
}
