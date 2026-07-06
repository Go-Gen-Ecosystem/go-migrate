/**
 * @file versioned_test.go
 * @description Behaviour tests for the versioned migration loader, semantic
 *              version comparator and automatic rollback generator.
 * @author Admilson B. F. Cossa
 */

package versioned

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- DefaultVersionedMigrationConfig ------------------------------------------------

func TestDefaultVersionedMigrationConfig(t *testing.T) {
	cfg := DefaultVersionedMigrationConfig()
	require.NotNil(t, cfg)
	assert.True(t, cfg.AutoGenerateRollback)
	assert.True(t, cfg.ValidateRollback)
	assert.True(t, cfg.StrictVersioning)
	assert.False(t, cfg.AllowOutOfOrder)
	assert.Equal(t, 50, cfg.MaxVersionLength)
	assert.NotEmpty(t, cfg.VersionPattern)
	assert.Greater(t, int64(cfg.RollbackTimeout), int64(0))
}

// --- NewVersionedMigrationLoader ----------------------------------------------------

func TestNewVersionedMigrationLoader_NilConfigUsesDefaults(t *testing.T) {
	loader := NewVersionedMigrationLoader(nil)
	require.NotNil(t, loader)
	require.NotNil(t, loader.FileMigrationLoader)
	require.NotNil(t, loader.rollbackGenerator)
	require.NotNil(t, loader.versionComparator)
}

func TestNewVersionedMigrationLoader_WithConfig(t *testing.T) {
	loader := NewVersionedMigrationLoader(&VersionedMigrationConfig{VersionPattern: "custom"})
	require.NotNil(t, loader)
	require.NotNil(t, loader.rollbackGenerator)
}

// --- LoadVersionedMigrations (real files from disk) ---------------------------------

func TestLoadVersionedMigrations_ParsesSortsAndFilters(t *testing.T) {
	dir := t.TempDir()

	// Two versioned SQL migrations written out of order on disk.
	writeFile(t, dir, "V2__add_index.sql", "CREATE INDEX idx_users_email ON users(email);")
	writeFile(t, dir, "V1__create_users.sql", "CREATE TABLE users (id INTEGER PRIMARY KEY);")
	// A non-versioned file that must be ignored by the versioned filter.
	writeFile(t, dir, "notes.txt", "not a migration")

	loader := NewVersionedMigrationLoader(nil)
	migs, err := loader.LoadVersionedMigrations(dir)
	require.NoError(t, err)
	require.Len(t, migs, 2, "only the two versioned SQL files should be returned")

	// Sorted ascending by semantic version: V1 before V2.
	assert.Equal(t, "1", migs[0].Version)
	assert.Equal(t, "2", migs[1].Version)

	// Descriptions parsed from the filename (underscores become spaces).
	assert.Equal(t, "create users", migs[0].Description[:len("create users")])
	assert.Contains(t, migs[1].Description, "add index")

	// Rollback was auto-generated for the CREATE TABLE migration: it is
	// rollbackable (DROP TABLE) so CanRollback must be true and RollbackSQL set.
	assert.True(t, migs[0].CanRollback)
	assert.Contains(t, strings.ToUpper(migs[0].RollbackSQL), "DROP TABLE")
}

func TestLoadVersionedMigrations_EmptyDir(t *testing.T) {
	loader := NewVersionedMigrationLoader(nil)
	migs, err := loader.LoadVersionedMigrations(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, migs)
}

func TestLoadVersionedMigrations_MissingPath(t *testing.T) {
	loader := NewVersionedMigrationLoader(nil)
	_, err := loader.LoadVersionedMigrations(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err)
}

func TestLoadVersionedMigrations_TimestampStyle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "20240101120000_seed.sql", "INSERT INTO t VALUES (1);")

	loader := NewVersionedMigrationLoader(nil)
	migs, err := loader.LoadVersionedMigrations(dir)
	require.NoError(t, err)
	require.Len(t, migs, 1)
	assert.Equal(t, "20240101120000", migs[0].Version)
}

// --- SemanticVersionComparator ------------------------------------------------------

func TestSemanticVersionComparator_Compare(t *testing.T) {
	c := NewSemanticVersionComparator()
	assert.Equal(t, -1, c.Compare("1.0.0", "1.0.1"))
	assert.Equal(t, 1, c.Compare("2.0", "1.9"))
	assert.Equal(t, 0, c.Compare("1.2.3", "1.2.3"))
	// Differing lengths: 1 == 1.0
	assert.Equal(t, 0, c.Compare("1", "1.0"))
	assert.Equal(t, -1, c.Compare("1", "1.1"))
}

func TestSemanticVersionComparator_IsValid(t *testing.T) {
	c := NewSemanticVersionComparator()
	assert.True(t, c.IsValid("1.2.3"))
	assert.True(t, c.IsValid("20240101120000")) // 14-digit timestamp
	assert.False(t, c.IsValid(""))
	assert.False(t, c.IsValid("1.x.3"))
	assert.False(t, c.IsValid("abc"))
}

func TestSemanticVersionComparator_GetNextPrevious(t *testing.T) {
	c := NewSemanticVersionComparator()
	assert.Equal(t, "1.0.4", c.GetNext("1.0.3"))
	assert.Equal(t, "1.0.2", c.GetPrevious("1.0.3"))

	// Timestamp increments/decrements preserve the 14-digit zero-padded width.
	next := c.GetNext("20240101120000")
	assert.Len(t, next, 14)
	assert.Equal(t, "20240101120001", next)
	// Integer decrement of the 14-digit value, not clock arithmetic.
	prev := c.GetPrevious("20240101120000")
	assert.Equal(t, "20240101119999", prev)
}

// --- AutoRollbackGenerator ----------------------------------------------------------

func TestAutoRollbackGenerator_AnalyzeSQLChanges(t *testing.T) {
	g := NewAutoRollbackGenerator()
	sql := "CREATE TABLE users (id INT);\n" +
		"CREATE INDEX idx_u ON users(id);\n" +
		"ALTER TABLE users ADD COLUMN name TEXT;\n" +
		"DROP TABLE old_users;"

	changes, err := g.AnalyzeChanges(sql, string(types.MigrationTypeSQL))
	require.NoError(t, err)
	require.Len(t, changes, 4)

	byType := map[string]DatabaseChange{}
	for _, c := range changes {
		byType[c.Type] = c
	}

	// CREATE TABLE -> rollbackable DROP TABLE.
	require.Contains(t, byType, "CREATE_TABLE")
	assert.True(t, byType["CREATE_TABLE"].Rollbackable)
	assert.Contains(t, strings.ToUpper(byType["CREATE_TABLE"].RollbackSQL), "DROP TABLE")
	assert.Equal(t, "USERS", byType["CREATE_TABLE"].Object)

	// CREATE INDEX -> rollbackable DROP INDEX.
	require.Contains(t, byType, "CREATE_INDEX")
	assert.True(t, byType["CREATE_INDEX"].Rollbackable)

	// ALTER TABLE and DROP TABLE are NOT auto-rollbackable.
	require.Contains(t, byType, "ALTER_TABLE")
	assert.False(t, byType["ALTER_TABLE"].Rollbackable)
	require.Contains(t, byType, "DROP_TABLE")
	assert.False(t, byType["DROP_TABLE"].Rollbackable)
}

func TestAutoRollbackGenerator_AnalyzeChanges_UnsupportedFormat(t *testing.T) {
	g := NewAutoRollbackGenerator()
	_, err := g.AnalyzeChanges("whatever", "toml")
	assert.Error(t, err)
}

func TestAutoRollbackGenerator_GenerateRollback_SQLReversible(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{
		ID:      "m1",
		Version: "1",
		Type:    types.MigrationTypeSQL,
		Script:  "CREATE TABLE a (id INT);\nCREATE TABLE b (id INT);",
	}

	rb, err := g.GenerateRollback(context.Background(), mig)
	require.NoError(t, err)
	require.NotNil(t, rb)
	assert.True(t, rb.IsAutomatic)
	assert.False(t, rb.RequiresManual, "all changes are reversible so no manual step needed")

	// Rollback content drops both tables, and in reverse order: b before a.
	up := strings.ToUpper(rb.Content)
	assert.Contains(t, up, "DROP TABLE IF EXISTS B")
	assert.Contains(t, up, "DROP TABLE IF EXISTS A")
	assert.Less(t, strings.Index(up, "DROP TABLE IF EXISTS B"), strings.Index(up, "DROP TABLE IF EXISTS A"))
}

func TestAutoRollbackGenerator_GenerateRollback_RequiresManual(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{
		ID:     "m2",
		Type:   types.MigrationTypeSQL,
		Script: "DROP TABLE users;", // not reversible
	}

	rb, err := g.GenerateRollback(context.Background(), mig)
	require.NoError(t, err)
	assert.True(t, rb.RequiresManual)
	require.NotEmpty(t, rb.Warnings)
}

func TestAutoRollbackGenerator_GenerateRollback_UnsupportedType(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{ID: "m3", Type: types.MigrationType("go"), Script: "x"}
	_, err := g.GenerateRollback(context.Background(), mig)
	assert.Error(t, err)
}

func TestAutoRollbackGenerator_GenerateRollback_XML(t *testing.T) {
	g := NewAutoRollbackGenerator()
	// A well-formed (empty) changelog document parses cleanly and drives the
	// XML analyze + generate branches.
	mig := &types.Migration{
		ID:     "x1",
		Type:   types.MigrationTypeXML,
		Script: `<databaseChangeLog></databaseChangeLog>`,
	}
	rb, err := g.GenerateRollback(context.Background(), mig)
	require.NoError(t, err)
	require.NotNil(t, rb)
	assert.Equal(t, string(types.MigrationTypeXML), rb.Format)
	// The generated rollback content is a serialized (empty) changelog.
	assert.Contains(t, rb.Content, "databaseChangeLog")
}

// NOTE: analyzeYAMLChanges / analyzeJSONChanges validate the payload as
// YAML/JSON but then delegate change analysis to analyzeXMLChanges(content),
// re-parsing the same raw non-XML content as XML. As a result, YAML/JSON
// rollback generation currently fails for any content that is not also valid
// XML. These tests pin that real behaviour (see the flagged defect).

func TestAutoRollbackGenerator_GenerateRollback_YAML_DelegatesToXMLParser(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{
		ID:     "y1",
		Type:   types.MigrationTypeYAML,
		Script: "changeSets: []\n", // valid YAML, not valid XML
	}
	_, err := g.GenerateRollback(context.Background(), mig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse XML")
}

func TestAutoRollbackGenerator_GenerateRollback_JSON_DelegatesToXMLParser(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{
		ID:     "j1",
		Type:   types.MigrationTypeJSON,
		Script: `{"changeSets": []}`, // valid JSON, not valid XML
	}
	_, err := g.GenerateRollback(context.Background(), mig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse XML")
}

func TestAutoRollbackGenerator_GenerateRollback_YAML_InvalidYAML(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{ID: "y2", Type: types.MigrationTypeYAML, Script: "\t: : bad"}
	_, err := g.GenerateRollback(context.Background(), mig)
	require.Error(t, err)
}

func TestAutoRollbackGenerator_GenerateRollback_BadXML(t *testing.T) {
	g := NewAutoRollbackGenerator()
	mig := &types.Migration{ID: "bx", Type: types.MigrationTypeXML, Script: "<not-closed"}
	_, err := g.GenerateRollback(context.Background(), mig)
	assert.Error(t, err)
}

func TestAutoRollbackGenerator_ValidateRollback(t *testing.T) {
	g := NewAutoRollbackGenerator()

	// Empty content is invalid.
	err := g.ValidateRollback(context.Background(), &types.Migration{}, &RollbackScript{})
	assert.Error(t, err)

	// Content present, no errors -> valid.
	err = g.ValidateRollback(context.Background(), &types.Migration{}, &RollbackScript{Content: "DROP TABLE a;"})
	assert.NoError(t, err)

	// Content present but generation errors recorded -> invalid.
	err = g.ValidateRollback(context.Background(), &types.Migration{}, &RollbackScript{
		Content: "x", Errors: []string{"boom"},
	})
	assert.Error(t, err)
}

// --- helpers ------------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
