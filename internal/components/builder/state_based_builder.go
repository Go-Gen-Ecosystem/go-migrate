/**
 * @file state_based_builder.go
 * @description Builder API for state-based migrations following the Builder pattern
 * @author Admilson B. F. Cossa
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/builder/state_based_builder.go
 */

package builder

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/state"
)

// StateBasedMigrationBuilder provides fluent API for building state-based migrations.
//
// The concrete *state.DefaultStateBasedMigrator returned by the state package
// constructs and owns its own parser, differ, generator, validator and extractor.
// The custom-component setters on this builder are retained for API compatibility
// and to surface intent, but the migrator itself is built from (db, config); the
// dialect is tracked here on the builder because state.StateBasedConfig does not
// carry a dialect field.
type StateBasedMigrationBuilder struct {
	config    *state.StateBasedConfig
	db        *sql.DB
	dialect   string
	parser    state.SchemaParser
	differ    state.SchemaDiffer
	generator state.MigrationGenerator
	validator state.SchemaValidator
	extractor state.SchemaExtractor
	migrator  *state.DefaultStateBasedMigrator
	errors    []error
}

// NewStateBasedMigrationBuilder creates a new builder instance
func NewStateBasedMigrationBuilder() *StateBasedMigrationBuilder {
	return &StateBasedMigrationBuilder{
		config: &state.StateBasedConfig{
			Enabled:            true,
			SchemaFormat:       state.FormatYAML,
			AutoGenerate:       false,
			AutoApply:          false,
			AllowDestructive:   false,
			RequireReview:      true,
			MigrationOutputDir: "migrations",
		},
		dialect: "postgres",
		errors:  make([]error, 0),
	}
}

// WithDatabase sets the database connection
func (b *StateBasedMigrationBuilder) WithDatabase(db *sql.DB) *StateBasedMigrationBuilder {
	b.db = db
	return b
}

// WithSchemaPath sets the path to the schema file
func (b *StateBasedMigrationBuilder) WithSchemaPath(path string) *StateBasedMigrationBuilder {
	b.config.SchemaPath = path
	return b
}

// WithSchemaFormat sets the schema file format (yaml or json)
func (b *StateBasedMigrationBuilder) WithSchemaFormat(format state.SchemaFormat) *StateBasedMigrationBuilder {
	b.config.SchemaFormat = format
	return b
}

// WithDialect sets the database dialect (postgres, mysql, sqlite)
func (b *StateBasedMigrationBuilder) WithDialect(dialect string) *StateBasedMigrationBuilder {
	b.dialect = dialect
	return b
}

// WithAutoGenerate enables/disables automatic migration generation
func (b *StateBasedMigrationBuilder) WithAutoGenerate(enabled bool) *StateBasedMigrationBuilder {
	b.config.AutoGenerate = enabled
	return b
}

// WithAutoApply enables/disables automatic migration application
func (b *StateBasedMigrationBuilder) WithAutoApply(enabled bool) *StateBasedMigrationBuilder {
	b.config.AutoApply = enabled
	return b
}

// WithAllowDestructive allows/disallows destructive schema changes
func (b *StateBasedMigrationBuilder) WithAllowDestructive(allow bool) *StateBasedMigrationBuilder {
	b.config.AllowDestructive = allow
	return b
}

// WithRequireReview requires/skips manual review for migrations
func (b *StateBasedMigrationBuilder) WithRequireReview(require bool) *StateBasedMigrationBuilder {
	b.config.RequireReview = require
	return b
}

// WithMigrationOutputDir sets the output directory for generated migrations
func (b *StateBasedMigrationBuilder) WithMigrationOutputDir(dir string) *StateBasedMigrationBuilder {
	b.config.MigrationOutputDir = dir
	return b
}

// WithParser sets a custom schema parser
func (b *StateBasedMigrationBuilder) WithParser(parser state.SchemaParser) *StateBasedMigrationBuilder {
	b.parser = parser
	return b
}

// WithDiffer sets a custom schema differ
func (b *StateBasedMigrationBuilder) WithDiffer(differ state.SchemaDiffer) *StateBasedMigrationBuilder {
	b.differ = differ
	return b
}

// WithGenerator sets a custom migration generator
func (b *StateBasedMigrationBuilder) WithGenerator(generator state.MigrationGenerator) *StateBasedMigrationBuilder {
	b.generator = generator
	return b
}

// WithValidator sets a custom schema validator
func (b *StateBasedMigrationBuilder) WithValidator(validator state.SchemaValidator) *StateBasedMigrationBuilder {
	b.validator = validator
	return b
}

// WithExtractor sets a custom schema extractor
func (b *StateBasedMigrationBuilder) WithExtractor(extractor state.SchemaExtractor) *StateBasedMigrationBuilder {
	b.extractor = extractor
	return b
}

// Build constructs the state-based migrator with all configured components.
//
// It returns the concrete *state.DefaultStateBasedMigrator. The state package's
// migrator owns and wires its internal components from (db, config), so the
// builder's custom-component setters are validated/initialized for API symmetry
// but are not injected into the migrator.
func (b *StateBasedMigrationBuilder) Build() (*state.DefaultStateBasedMigrator, error) {
	// Validate configuration
	if err := b.validate(); err != nil {
		return nil, fmt.Errorf("builder validation failed: %w", err)
	}

	// Initialize default components if not provided
	b.initializeDefaults()

	// Create the migrator. The constructor internally builds its own parser,
	// differ, generator, validator and extractor from the database and config.
	b.migrator = state.NewDefaultStateBasedMigrator(b.db, b.config)

	return b.migrator, nil
}

// validate ensures all required components are configured
func (b *StateBasedMigrationBuilder) validate() error {
	if b.db == nil {
		return fmt.Errorf("database connection is required")
	}

	if b.config.SchemaPath == "" {
		return fmt.Errorf("schema path is required")
	}

	if b.dialect == "" {
		return fmt.Errorf("database dialect is required")
	}

	return nil
}

// initializeDefaults creates default components if not provided
func (b *StateBasedMigrationBuilder) initializeDefaults() {
	if b.parser == nil {
		b.parser = state.NewDefaultSchemaParser(nil)
	}

	if b.differ == nil {
		b.differ = state.NewDefaultSchemaDiffer(b.config)
	}

	if b.generator == nil {
		b.generator = state.NewDefaultMigrationGenerator(b.dialect)
	}

	if b.validator == nil {
		// A nil naming-convention set makes the validator fall back to its
		// built-in defaults, which is the desired behaviour here.
		b.validator = state.NewDefaultSchemaValidator(nil)
	}

	if b.extractor == nil {
		switch b.dialect {
		case "postgres", "postgresql":
			b.extractor = state.NewPostgreSQLSchemaExtractor(b.db)
		default:
			b.extractor = state.NewPostgreSQLSchemaExtractor(b.db)
		}
	}
}

// StateBasedMigrationChain provides chainable operations for state-based migrations
type StateBasedMigrationChain struct {
	builder  *StateBasedMigrationBuilder
	migrator *state.DefaultStateBasedMigrator
	ctx      context.Context
}

// NewStateBasedMigrationChain creates a new migration chain
func NewStateBasedMigrationChain(ctx context.Context, db *sql.DB) *StateBasedMigrationChain {
	return &StateBasedMigrationChain{
		builder: NewStateBasedMigrationBuilder().WithDatabase(db),
		ctx:     ctx,
	}
}

// WithSchemaPath sets the schema path and returns the chain
func (c *StateBasedMigrationChain) WithSchemaPath(path string) *StateBasedMigrationChain {
	c.builder.WithSchemaPath(path)
	return c
}

// WithDialect sets the dialect and returns the chain
func (c *StateBasedMigrationChain) WithDialect(dialect string) *StateBasedMigrationChain {
	c.builder.WithDialect(dialect)
	return c
}

// EnableAutoApply enables automatic migration application
func (c *StateBasedMigrationChain) EnableAutoApply() *StateBasedMigrationChain {
	c.builder.WithAutoApply(true)
	return c
}

// AllowDestructive allows destructive changes
func (c *StateBasedMigrationChain) AllowDestructive() *StateBasedMigrationChain {
	c.builder.WithAllowDestructive(true)
	return c
}

// Validate validates the schema file
func (c *StateBasedMigrationChain) Validate() (*state.SchemaValidationResult, error) {
	if c.migrator == nil {
		migrator, err := c.builder.Build()
		if err != nil {
			return nil, err
		}
		c.migrator = migrator
	}

	return c.migrator.ValidateSchemaFile(c.ctx, c.builder.config.SchemaPath)
}

// Preview generates a preview of schema changes
func (c *StateBasedMigrationChain) Preview() (*state.SchemaDiff, error) {
	if c.migrator == nil {
		migrator, err := c.builder.Build()
		if err != nil {
			return nil, err
		}
		c.migrator = migrator
	}

	return c.migrator.Preview(c.ctx, c.builder.config.SchemaPath)
}

// Plan generates a migration plan
func (c *StateBasedMigrationChain) Plan() (*state.GeneratedMigration, error) {
	if c.migrator == nil {
		migrator, err := c.builder.Build()
		if err != nil {
			return nil, err
		}
		c.migrator = migrator
	}

	return c.migrator.Plan(c.ctx, c.builder.config.SchemaPath)
}

// Sync synchronizes the database with the schema
func (c *StateBasedMigrationChain) Sync(dryRun bool) error {
	if c.migrator == nil {
		migrator, err := c.builder.Build()
		if err != nil {
			return err
		}
		c.migrator = migrator
	}

	_, err := c.migrator.Sync(c.ctx, c.builder.config.SchemaPath, dryRun)
	return err
}

// Export exports the current database schema
func (c *StateBasedMigrationChain) Export(outputPath string, format state.SchemaFormat) error {
	if c.migrator == nil {
		migrator, err := c.builder.Build()
		if err != nil {
			return err
		}
		c.migrator = migrator
	}

	return c.migrator.Export(c.ctx, outputPath, format)
}
