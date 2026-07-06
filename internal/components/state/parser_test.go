/**
 * @file parser_test.go
 * @description Unit tests for schema parser
 * @author Claude Code
 * @date 2025-01-01
 */

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewDefaultSchemaParser(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	if parser == nil {
		t.Fatal("NewDefaultSchemaParser returned nil")
	}
}

func TestParseContent_YAML_Valid(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	content := `
version: "1.0.0"
name: "test_db"
tables:
  - name: users
    columns:
      - name: id
        type: integer
        nullable: false
      - name: email
        type: varchar
        length: 255
    primary_key:
      - id
`

	schema, err := parser.ParseContent(ctx, content, FormatYAML)
	if err != nil {
		t.Fatalf("ParseContent failed: %v", err)
	}

	if schema.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", schema.Version)
	}

	if schema.Name != "test_db" {
		t.Errorf("Expected name test_db, got %s", schema.Name)
	}

	if len(schema.Tables) != 1 {
		t.Fatalf("Expected 1 table, got %d", len(schema.Tables))
	}

	table := schema.Tables[0]
	if table.Name != "users" {
		t.Errorf("Expected table name users, got %s", table.Name)
	}

	if len(table.Columns) != 2 {
		t.Errorf("Expected 2 columns, got %d", len(table.Columns))
	}
}

func TestParseContent_JSON_Valid(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	content := `{
  "version": "1.0.0",
  "name": "test_db",
  "tables": [
    {
      "name": "users",
      "columns": [
        {
          "name": "id",
          "type": "integer",
          "nullable": false
        }
      ]
    }
  ]
}`

	schema, err := parser.ParseContent(ctx, content, FormatJSON)
	if err != nil {
		t.Fatalf("ParseContent failed: %v", err)
	}

	if schema.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", schema.Version)
	}

	if len(schema.Tables) != 1 {
		t.Fatalf("Expected 1 table, got %d", len(schema.Tables))
	}
}

func TestParseContent_EmptyContent(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	_, err := parser.ParseContent(ctx, "", FormatYAML)
	if err == nil {
		t.Error("Expected error for empty content")
	}
}

func TestParseFile_Valid(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	// Create temp file
	tmpDir := t.TempDir()
	schemaFile := filepath.Join(tmpDir, "schema.yaml")

	content := `
version: "1.0.0"
name: "test"
tables:
  - name: test_table
    columns:
      - name: id
        type: integer
`

	if err := os.WriteFile(schemaFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	schema, err := parser.Parse(ctx, schemaFile)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if schema.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", schema.Version)
	}
}

func TestParseFile_NotFound(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)
	ctx := context.Background()

	_, err := parser.Parse(ctx, "nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestSupportedFormats(t *testing.T) {
	parser := NewDefaultSchemaParser(nil)

	formats := parser.SupportedFormats()
	if len(formats) < 2 {
		t.Errorf("Expected at least 2 supported formats, got %d", len(formats))
	}

	hasYAML := false
	hasJSON := false
	for _, format := range formats {
		if format == FormatYAML {
			hasYAML = true
		}
		if format == FormatJSON {
			hasJSON = true
		}
	}

	if !hasYAML {
		t.Error("YAML format should be supported")
	}

	if !hasJSON {
		t.Error("JSON format should be supported")
	}
}

func TestSerializer_SerializeYAML(t *testing.T) {
	serializer := NewDefaultSchemaSerializer()
	ctx := context.Background()

	schema := &DatabaseSchema{
		Version: "1.0.0",
		Name:    "test",
		Tables: []Table{
			{
				Name: "users",
				Columns: []Column{
					{Name: "id", Type: "integer"},
				},
			},
		},
	}

	data, err := serializer.Serialize(ctx, schema, FormatYAML)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Serialized data is empty")
	}

	// Verify it can be deserialized back
	parsed, err := serializer.Deserialize(ctx, data, FormatYAML)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	if parsed.Version != schema.Version {
		t.Errorf("Version mismatch: expected %s, got %s", schema.Version, parsed.Version)
	}
}

func TestSerializer_SerializeJSON(t *testing.T) {
	serializer := NewDefaultSchemaSerializer()
	ctx := context.Background()

	schema := &DatabaseSchema{
		Version: "1.0.0",
		Name:    "test",
		Tables:  []Table{},
	}

	data, err := serializer.Serialize(ctx, schema, FormatJSON)
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Serialized data is empty")
	}
}

func TestSerializer_SerializeToFile(t *testing.T) {
	serializer := NewDefaultSchemaSerializer()
	ctx := context.Background()

	schema := &DatabaseSchema{
		Version: "1.0.0",
		Name:    "test",
		Tables:  []Table{},
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output.yaml")

	err := serializer.SerializeToFile(ctx, schema, outputPath, FormatYAML)
	if err != nil {
		t.Fatalf("SerializeToFile failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("Output file was not created")
	}

	// Verify content
	loaded, err := serializer.DeserializeFromFile(ctx, outputPath)
	if err != nil {
		t.Fatalf("DeserializeFromFile failed: %v", err)
	}

	if loaded.Version != schema.Version {
		t.Errorf("Version mismatch after round-trip")
	}
}

func TestSerializer_NilSchema(t *testing.T) {
	serializer := NewDefaultSchemaSerializer()
	ctx := context.Background()

	_, err := serializer.Serialize(ctx, nil, FormatYAML)
	if err == nil {
		t.Error("Expected error for nil schema")
	}
}
