/**
 * @file state_based_builder_test.go
 * @description Behaviour tests for the StateBasedMigrationBuilder fluent API and
 *              its validation, driven against a real in-memory SQLite handle.
 * @author Admilson B. F. Cossa
 */

package builder

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openSQLDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestStateBasedBuilder_Defaults(t *testing.T) {
	// A default builder without a database fails validation on Build; this
	// proves the constructor wires a config but leaves required inputs unset.
	b := NewStateBasedMigrationBuilder()
	require.NotNil(t, b)
	_, err := b.Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection is required")
}

func TestStateBasedBuilder_Build_Success(t *testing.T) {
	db := openSQLDB(t)

	migrator, err := NewStateBasedMigrationBuilder().
		WithDatabase(db).
		WithSchemaPath("schema.yaml").
		WithDialect("sqlite").
		WithSchemaFormat(state.FormatYAML).
		WithAutoGenerate(true).
		WithAutoApply(false).
		WithAllowDestructive(false).
		WithRequireReview(true).
		WithMigrationOutputDir("out/migrations").
		Build()

	require.NoError(t, err)
	require.NotNil(t, migrator)
	// Build returns the concrete state migrator.
	var _ *state.DefaultStateBasedMigrator = migrator
}

func TestStateBasedBuilder_Build_MissingDatabase(t *testing.T) {
	_, err := NewStateBasedMigrationBuilder().
		WithSchemaPath("schema.yaml").
		WithDialect("sqlite").
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection is required")
}

func TestStateBasedBuilder_Build_MissingSchemaPath(t *testing.T) {
	db := openSQLDB(t)
	_, err := NewStateBasedMigrationBuilder().
		WithDatabase(db).
		WithDialect("sqlite").
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema path is required")
}

func TestStateBasedBuilder_Build_MissingDialect(t *testing.T) {
	db := openSQLDB(t)
	_, err := NewStateBasedMigrationBuilder().
		WithDatabase(db).
		WithSchemaPath("schema.yaml").
		WithDialect("").
		Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dialect is required")
}

func TestStateBasedBuilder_CustomComponentSetters(t *testing.T) {
	db := openSQLDB(t)
	// Supplying custom components must not break Build (they are validated for
	// API symmetry; the migrator builds its own internals).
	migrator, err := NewStateBasedMigrationBuilder().
		WithDatabase(db).
		WithSchemaPath("schema.yaml").
		WithDialect("postgres").
		WithParser(state.NewDefaultSchemaParser(nil)).
		WithDiffer(state.NewDefaultSchemaDiffer(nil)).
		WithGenerator(state.NewDefaultMigrationGenerator("postgresql")).
		WithValidator(state.NewDefaultSchemaValidator(nil)).
		WithExtractor(state.NewPostgreSQLSchemaExtractor(db)).
		Build()
	require.NoError(t, err)
	require.NotNil(t, migrator)
}

func TestStateBasedBuilder_Chain_Construction(t *testing.T) {
	db := openSQLDB(t)
	chain := NewStateBasedMigrationChain(context.Background(), db).
		WithSchemaPath("schema.yaml").
		WithDialect("sqlite").
		EnableAutoApply().
		AllowDestructive()
	require.NotNil(t, chain)
}
