/**
 * @file parser.go
 * @description Schema file parser implementation
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/parser.go
 */

package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSchemaParser implements SchemaParser interface
type DefaultSchemaParser struct {
	validator SchemaValidator
}

// NewDefaultSchemaParser creates a new schema parser
func NewDefaultSchemaParser(validator SchemaValidator) *DefaultSchemaParser {
	return &DefaultSchemaParser{
		validator: validator,
	}
}

// Parse loads and parses a schema file
func (p *DefaultSchemaParser) Parse(ctx context.Context, path string) (*DatabaseSchema, error) {
	if path == "" {
		return nil, fmt.Errorf("schema path cannot be empty")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("schema file not found: %s", path)
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	// Determine format from extension
	format := p.detectFormat(path)

	// Parse content
	schema, err := p.ParseContent(ctx, string(content), format)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema file %s: %w", path, err)
	}

	// Validate if validator is set
	if p.validator != nil {
		result, err := p.validator.ValidateSchema(ctx, schema)
		if err != nil {
			return nil, fmt.Errorf("schema validation failed: %w", err)
		}
		if !result.Valid {
			return nil, fmt.Errorf("invalid schema: %v", result.Errors)
		}
	}

	return schema, nil
}

// ParseContent parses schema from string content
func (p *DefaultSchemaParser) ParseContent(ctx context.Context, content string, format SchemaFormat) (*DatabaseSchema, error) {
	if content == "" {
		return nil, fmt.Errorf("schema content cannot be empty")
	}

	var schema DatabaseSchema

	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal([]byte(content), &schema); err != nil {
			return nil, fmt.Errorf("failed to parse YAML schema: %w", err)
		}

	case FormatJSON:
		if err := json.Unmarshal([]byte(content), &schema); err != nil {
			return nil, fmt.Errorf("failed to parse JSON schema: %w", err)
		}

	case FormatHCL:
		return nil, fmt.Errorf("HCL format not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported schema format: %s", format)
	}

	// Set defaults
	p.setDefaults(&schema)

	return &schema, nil
}

// Validate validates schema syntax and structure
func (p *DefaultSchemaParser) Validate(ctx context.Context, schema *DatabaseSchema) (*SchemaValidationResult, error) {
	if p.validator == nil {
		return &SchemaValidationResult{
			Valid:    true,
			Warnings: []ValidationWarning{{
				Migration: "",
				Type:      "configuration",
				Message:   "no validator configured - skipping validation",
				Code:      "VAL016",
			}},
		}, nil
	}

	return p.validator.ValidateSchema(ctx, schema)
}

// SupportedFormats returns list of supported schema formats
func (p *DefaultSchemaParser) SupportedFormats() []SchemaFormat {
	return []SchemaFormat{FormatYAML, FormatJSON}
}

// detectFormat determines schema format from file extension
func (p *DefaultSchemaParser) detectFormat(path string) SchemaFormat {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".yaml", ".yml":
		return FormatYAML
	case ".json":
		return FormatJSON
	case ".hcl":
		return FormatHCL
	default:
		return FormatYAML // default to YAML
	}
}

// setDefaults sets default values for schema elements
func (p *DefaultSchemaParser) setDefaults(schema *DatabaseSchema) {
	// Set table defaults
	for i := range schema.Tables {
		table := &schema.Tables[i]

		// Set column defaults
		for j := range table.Columns {
			col := &table.Columns[j]

			// Default nullable to true if not specified
			if col.Type != "" && !col.AutoIncrement {
				// Nullable is already false by default (bool zero value)
			}
		}

		// Ensure indexes have table reference
		for j := range table.Indexes {
			if table.Indexes[j].Table == "" {
				table.Indexes[j].Table = table.Name
			}
		}

		// Ensure constraints have table reference
		for j := range table.Constraints {
			if table.Constraints[j].Table == "" {
				table.Constraints[j].Table = table.Name
			}
		}
	}

	// Set global index defaults
	for i := range schema.Indexes {
		if schema.Indexes[i].Type == "" {
			schema.Indexes[i].Type = "btree" // default index type
		}
	}
}

// DefaultSchemaSerializer implements SchemaSerializer interface
type DefaultSchemaSerializer struct{}

// NewDefaultSchemaSerializer creates a new schema serializer
func NewDefaultSchemaSerializer() *DefaultSchemaSerializer {
	return &DefaultSchemaSerializer{}
}

// Serialize converts DatabaseSchema to bytes
func (s *DefaultSchemaSerializer) Serialize(ctx context.Context, schema *DatabaseSchema, format SchemaFormat) ([]byte, error) {
	if schema == nil {
		return nil, fmt.Errorf("schema cannot be nil")
	}

	switch format {
	case FormatYAML:
		data, err := yaml.Marshal(schema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal schema to YAML: %w", err)
		}
		return data, nil

	case FormatJSON:
		data, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to marshal schema to JSON: %w", err)
		}
		return data, nil

	case FormatHCL:
		return nil, fmt.Errorf("HCL format not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported schema format: %s", format)
	}
}

// Deserialize converts bytes to DatabaseSchema
func (s *DefaultSchemaSerializer) Deserialize(ctx context.Context, data []byte, format SchemaFormat) (*DatabaseSchema, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}

	var schema DatabaseSchema

	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to unmarshal YAML schema: %w", err)
		}

	case FormatJSON:
		if err := json.Unmarshal(data, &schema); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON schema: %w", err)
		}

	case FormatHCL:
		return nil, fmt.Errorf("HCL format not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported schema format: %s", format)
	}

	return &schema, nil
}

// SerializeToFile writes schema to file
func (s *DefaultSchemaSerializer) SerializeToFile(ctx context.Context, schema *DatabaseSchema, path string, format SchemaFormat) error {
	if schema == nil {
		return fmt.Errorf("schema cannot be nil")
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	// Serialize schema
	data, err := s.Serialize(ctx, schema, format)
	if err != nil {
		return fmt.Errorf("failed to serialize schema: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write schema file: %w", err)
	}

	return nil
}

// DeserializeFromFile reads schema from file
func (s *DefaultSchemaSerializer) DeserializeFromFile(ctx context.Context, path string) (*DatabaseSchema, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file: %w", err)
	}

	// Detect format from extension
	ext := strings.ToLower(filepath.Ext(path))
	var format SchemaFormat

	switch ext {
	case ".yaml", ".yml":
		format = FormatYAML
	case ".json":
		format = FormatJSON
	case ".hcl":
		format = FormatHCL
	default:
		format = FormatYAML
	}

	// Deserialize
	schema, err := s.Deserialize(ctx, data, format)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize schema: %w", err)
	}

	return schema, nil
}
