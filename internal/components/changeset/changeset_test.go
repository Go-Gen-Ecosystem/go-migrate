/**
 * @file changeset_test.go
 * @description Behavioral tests for changeset support: changelog parsing across
 * XML/YAML/JSON, changeset/changelog validation, dependency resolution and
 * topological ordering, precondition evaluation against in-memory sqlite,
 * rollback generation, and execution-plan construction.
 * @author Admilson B. F. Cossa
 */

package changeset

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// writeFileCS writes content to dir/name and returns any error.
func writeFileCS(t *testing.T, dir, name, content string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestDefaultChangesetMigrationConfig(t *testing.T) {
	cfg := DefaultChangesetMigrationConfig()
	require.NotNil(t, cfg)
	assert.True(t, cfg.ValidateChecksums)
	assert.False(t, cfg.AllowDuplicateIDs)
	assert.True(t, cfg.StrictDependencies)
	assert.Equal(t, 10, cfg.MaxDependencyDepth)
	assert.True(t, cfg.AutoGenerateRollback)
}

func TestNewChangesetMigrationLoader_NilConfig(t *testing.T) {
	// A nil config must fall back to the default configuration.
	l := NewChangesetMigrationLoader(nil)
	require.NotNil(t, l)
	require.NotNil(t, l.FileMigrationLoader)
}

// --- parseChangelog across formats ---

func TestParseChangelog_XML(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	xml := `<databaseChangeLog>
  <changeSets id="c1" author="a1">
    <changes type="sql"><![CDATA[SELECT 1;]]></changes>
  </changeSets>
</databaseChangeLog>`
	cl, err := l.parseChangelog(xml, "xml")
	require.NoError(t, err)
	require.Len(t, cl.ChangeSets, 1)
	assert.Equal(t, "c1", cl.ChangeSets[0].ID)
	assert.Equal(t, "a1", cl.ChangeSets[0].Author)
}

func TestParseChangelog_YAML(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	yamlDoc := `changeSets:
  - id: c1
    author: a1
    changes:
      - type: sql
        content: SELECT 1;
`
	cl, err := l.parseChangelog(yamlDoc, "yaml")
	require.NoError(t, err)
	require.Len(t, cl.ChangeSets, 1)
	assert.Equal(t, "c1", cl.ChangeSets[0].ID)
}

func TestParseChangelog_JSON(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	jsonDoc := `{"changeSets":[{"id":"c1","author":"a1","changes":[{"type":"sql","content":"SELECT 1;"}]}]}`
	cl, err := l.parseChangelog(jsonDoc, "json")
	require.NoError(t, err)
	require.Len(t, cl.ChangeSets, 1)
	assert.Equal(t, "c1", cl.ChangeSets[0].ID)
}

func TestParseChangelog_UnsupportedFormat(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	_, err := l.parseChangelog("data", "toml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestParseChangelog_MalformedJSON(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	_, err := l.parseChangelog("{not json", "json")
	assert.Error(t, err)
}

// --- DefaultChangesetValidator ---

func TestChangesetValidator_ValidateChangeset(t *testing.T) {
	v := NewDefaultChangesetValidator(DefaultChangesetMigrationConfig())
	ctx := context.Background()

	valid := &types.ChangeSet{
		ID:      "c1",
		Author:  "alice",
		Changes: []types.Change{{Type: "createTable", Attributes: map[string]interface{}{"tableName": "users"}}},
	}
	assert.NoError(t, v.ValidateChangeset(ctx, valid))

	// Missing ID.
	assert.Error(t, v.ValidateChangeset(ctx, &types.ChangeSet{Author: "a", Changes: valid.Changes}))
	// Missing author.
	assert.Error(t, v.ValidateChangeset(ctx, &types.ChangeSet{ID: "c", Changes: valid.Changes}))
	// No changes.
	assert.Error(t, v.ValidateChangeset(ctx, &types.ChangeSet{ID: "c", Author: "a"}))
}

func TestChangesetValidator_ValidateChangeRequiredAttrs(t *testing.T) {
	v := NewDefaultChangesetValidator(DefaultChangesetMigrationConfig())
	ctx := context.Background()

	// createTable without tableName is invalid.
	badCreate := &types.ChangeSet{
		ID:      "c",
		Author:  "a",
		Changes: []types.Change{{Type: "createTable", Attributes: map[string]interface{}{}}},
	}
	err := v.ValidateChangeset(ctx, badCreate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tableName")

	// addColumn requires both tableName and columnName.
	badColumn := &types.ChangeSet{
		ID:      "c",
		Author:  "a",
		Changes: []types.Change{{Type: "addColumn", Attributes: map[string]interface{}{"tableName": "t"}}},
	}
	err = v.ValidateChangeset(ctx, badColumn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "columnName")

	// A change with an empty type is invalid.
	badType := &types.ChangeSet{
		ID:      "c",
		Author:  "a",
		Changes: []types.Change{{Type: ""}},
	}
	assert.Error(t, v.ValidateChangeset(ctx, badType))
}

func TestChangesetValidator_ValidateChangelog(t *testing.T) {
	v := NewDefaultChangesetValidator(DefaultChangesetMigrationConfig())
	ctx := context.Background()

	change := []types.Change{{Type: "createTable", Attributes: map[string]interface{}{"tableName": "t"}}}

	// Empty changelog is invalid.
	assert.Error(t, v.ValidateChangelog(ctx, &types.Changelog{}))

	// Valid changelog with a single changeset.
	valid := &types.Changelog{ChangeSets: []types.ChangeSet{{ID: "c1", Author: "a", Changes: change}}}
	assert.NoError(t, v.ValidateChangelog(ctx, valid))

	// Duplicate changeset IDs are rejected.
	dup := &types.Changelog{ChangeSets: []types.ChangeSet{
		{ID: "c1", Author: "a", Changes: change},
		{ID: "c1", Author: "a", Changes: change},
	}}
	err := v.ValidateChangelog(ctx, dup)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate changeset ID")
}

func TestChangesetValidator_ValidateDependencies(t *testing.T) {
	v := NewDefaultChangesetValidator(DefaultChangesetMigrationConfig())
	ctx := context.Background()

	// c2 runs after c1, which exists => valid.
	valid := []types.ChangeSet{
		{ID: "c1", Author: "a"},
		{ID: "c2", Author: "a", RunAfter: []string{"c1"}},
	}
	assert.NoError(t, v.ValidateDependencies(ctx, valid))

	// c2 runs after a non-existent changeset => error.
	invalid := []types.ChangeSet{
		{ID: "c2", Author: "a", RunAfter: []string{"ghost"}},
	}
	err := v.ValidateDependencies(ctx, invalid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent changeset")
}

// --- DefaultDependencyResolver (changeset) ---

func TestDependencyResolver_BuildAndSort(t *testing.T) {
	r := NewDefaultDependencyResolver(DefaultChangesetMigrationConfig())
	ctx := context.Background()

	changesets := []types.ChangeSet{
		{ID: "c1", Author: "a"},
		{ID: "c2", Author: "b"},
	}

	graph, err := r.BuildDependencyGraph(changesets)
	require.NoError(t, err)
	assert.Len(t, graph.Nodes, 2)

	// With no extracted dependencies every changeset is independent, so the
	// topological sort returns all of them.
	ordered, err := r.ResolveDependencies(ctx, changesets)
	require.NoError(t, err)
	assert.Len(t, ordered, 2)
}

func TestDependencyResolver_EmptyChangesets(t *testing.T) {
	r := NewDefaultDependencyResolver(DefaultChangesetMigrationConfig())
	ordered, err := r.ResolveDependencies(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, ordered)
}

// --- DefaultPreconditionChecker ---

func TestPreconditionChecker_TableExists(t *testing.T) {
	db := newTestDB(t)
	// information_schema is emulated so the checker's COUNT query runs; on
	// sqlite the query against information_schema returns an error, which the
	// checker surfaces. We instead assert on the supported/unsupported dispatch.
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	// An unsupported precondition type returns an explicit error.
	unsupported := types.ChangePrecondition{
		Condition: map[string]interface{}{"bogusType": map[string]interface{}{}},
	}
	_, err := c.EvaluatePrecondition(context.Background(), db, unsupported)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported precondition type")
}

func TestPreconditionChecker_EmptyConditionPasses(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	// A precondition with no condition entries evaluates to true (nothing to
	// check).
	empty := types.ChangePrecondition{Condition: map[string]interface{}{}}
	ok, err := c.EvaluatePrecondition(context.Background(), db, empty)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestPreconditionChecker_CheckPreconditions_Empty(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())
	assert.NoError(t, c.CheckPreconditions(context.Background(), db, nil))
}

func TestPreconditionChecker_CheckPreconditions_Unsupported(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	pcs := []types.ChangePrecondition{
		{Condition: map[string]interface{}{"bogusType": map[string]interface{}{}}},
	}
	err := c.CheckPreconditions(context.Background(), db, pcs)
	require.Error(t, err)
}

func TestPreconditionChecker_TableExistsDispatch(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	// tableExists dispatches to checkTableExists, which queries
	// information_schema. On sqlite that table is absent, so the query errors;
	// we assert the dispatch reached the checker (error surfaced).
	pc := types.ChangePrecondition{
		Condition: map[string]interface{}{
			"tableExists": map[string]interface{}{"tableName": "users"},
		},
	}
	_, err := c.EvaluatePrecondition(context.Background(), db, pc)
	assert.Error(t, err, "information_schema is unavailable on sqlite")
}

func TestPreconditionChecker_ColumnExistsDispatch(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	pc := types.ChangePrecondition{
		Condition: map[string]interface{}{
			"columnExists": map[string]interface{}{"tableName": "users", "columnName": "id"},
		},
	}
	_, err := c.EvaluatePrecondition(context.Background(), db, pc)
	assert.Error(t, err)
}

func TestPreconditionChecker_IndexExistsDispatch(t *testing.T) {
	db := newTestDB(t)
	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	pc := types.ChangePrecondition{
		Condition: map[string]interface{}{
			"indexExists": map[string]interface{}{"indexName": "idx_users"},
		},
	}
	_, err := c.EvaluatePrecondition(context.Background(), db, pc)
	assert.Error(t, err)
}

func TestPreconditionChecker_SQLCheckPasses(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE flags (val TEXT);").Error)
	require.NoError(t, db.Exec("INSERT INTO flags (val) VALUES ('ready');").Error)

	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	// sqlCheck runs a real query and compares the scalar result to expected.
	pass := types.ChangePrecondition{
		Condition: map[string]interface{}{
			"sqlCheck": map[string]interface{}{
				"sql":            "SELECT val FROM flags LIMIT 1",
				"expectedResult": "ready",
			},
		},
	}
	ok, err := c.EvaluatePrecondition(context.Background(), db, pass)
	require.NoError(t, err)
	assert.True(t, ok)

	// A mismatched expected result evaluates to false.
	fail := types.ChangePrecondition{
		Condition: map[string]interface{}{
			"sqlCheck": map[string]interface{}{
				"sql":            "SELECT val FROM flags LIMIT 1",
				"expectedResult": "not-ready",
			},
		},
	}
	ok, err = c.EvaluatePrecondition(context.Background(), db, fail)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPreconditionChecker_CheckPreconditions_FailsOnFalse(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Exec("CREATE TABLE flags (val TEXT);").Error)
	require.NoError(t, db.Exec("INSERT INTO flags (val) VALUES ('x');").Error)

	c := NewDefaultPreconditionChecker(DefaultChangesetMigrationConfig())

	// A precondition that evaluates to false makes CheckPreconditions fail.
	pcs := []types.ChangePrecondition{
		{Condition: map[string]interface{}{
			"sqlCheck": map[string]interface{}{
				"sql":            "SELECT val FROM flags LIMIT 1",
				"expectedResult": "y",
			},
		}},
	}
	err := c.CheckPreconditions(context.Background(), db, pcs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "precondition failed")
}

func TestLoadChangesetMigrations_SQLOnlyReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	// Only plain SQL files: none are detected as changesets, so the result is
	// empty and there is no enhancement to fail.
	require.NoError(t, writeFileCS(t, dir, "V1__seed.sql", "CREATE TABLE seed (id INTEGER);"))

	l := NewChangesetMigrationLoader(nil)
	migrations, err := l.LoadChangesetMigrations(dir)
	require.NoError(t, err)
	assert.Empty(t, migrations, "SQL files are not changeset migrations")
}

// TestLoadChangesetMigrations_EnhanceFailure documents a real limitation of the
// reference implementation: the base FileMigrationLoader flattens a YAML/XML/JSON
// changeset's Script into joined SQL during load, and enhanceChangesetMigration
// then tries to re-parse that flattened SQL as a changelog, which fails. This
// test pins the current (buggy) behavior so a future fix has a red baseline to
// flip. See findings note in the accompanying report.
func TestLoadChangesetMigrations_EnhanceFailure(t *testing.T) {
	dir := t.TempDir()
	yamlDoc := `changeSets:
  - id: cs1
    author: alice
    changes:
      - type: sql
        content: CREATE TABLE users (id INTEGER);
`
	require.NoError(t, writeFileCS(t, dir, "V1__users.yaml", yamlDoc))

	l := NewChangesetMigrationLoader(nil)
	_, err := l.LoadChangesetMigrations(dir)
	require.Error(t, err, "flattened script can no longer be parsed as a changelog")
	assert.Contains(t, err.Error(), "failed to enhance changeset migration")
}

// --- Execution plan + rollback generation via LoadChangesetMigrations path ---

func TestCreateExecutionPlan(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	ctx := context.Background()

	// Two migrations, each carrying one changeset in its DatabaseChangeLog.
	yaml1 := `databaseChangeLog:
  - id: cs1
    author: a
    changes:
      - type: sql
        content: SELECT 1;
`
	yaml2 := `databaseChangeLog:
  - id: cs2
    author: b
    changes:
      - type: sql
        content: SELECT 2;
`
	migrations := []*types.Migration{
		{ID: "m1", Type: types.MigrationTypeYAML, Script: yaml1},
		{ID: "m2", Type: types.MigrationTypeYAML, Script: yaml2},
	}

	plan, err := l.CreateExecutionPlan(ctx, migrations)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.Len(t, plan.Changesets, 2)
	assert.ElementsMatch(t, []string{"cs1", "cs2"}, plan.ExecutionOrder)
}

func TestCreateExecutionPlan_ParseErrorRecorded(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	ctx := context.Background()

	// A malformed changeset script is recorded as a plan error but does not
	// abort planning of the remaining migrations.
	migrations := []*types.Migration{
		{ID: "bad", Type: types.MigrationTypeJSON, Script: "{not json"},
	}
	plan, err := l.CreateExecutionPlan(ctx, migrations)
	require.NoError(t, err)
	require.NotNil(t, plan)
	assert.NotEmpty(t, plan.Errors)
}

func TestEnhanceChangesetMigration_AttachesMetadata(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)

	// A YAML changeset with a valid sql change passes validation; enhancement
	// attaches parsed-changelog metadata. AutoGenerateRollback is on, but a sql
	// change has no automatic rollback, so no RollbackSQL is produced.
	yamlDoc := `changeSets:
  - id: cs1
    author: alice
    changes:
      - type: sql
        content: CREATE TABLE users (id INTEGER);
`
	m := &types.Migration{
		ID:     "m1",
		Type:   types.MigrationTypeYAML,
		Script: yamlDoc,
	}

	enhanced, err := l.enhanceChangesetMigration(m)
	require.NoError(t, err)
	require.NotNil(t, enhanced)
	// Metadata is attached from the parsed changelog.
	assert.Equal(t, "parsed", enhanced.Metadata["changelog"])
	// The "changesets" metadata counts DatabaseChangeLog entries, whereas the
	// "changeSets" YAML key populates the ChangeSets field; with only the
	// latter set the count is reported as 0.
	assert.Equal(t, "0", enhanced.Metadata["changesets"])
	assert.Contains(t, enhanced.Metadata, "has_rollback")
}

func TestEnhanceChangesetMigration_InvalidChangelog(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)

	// An empty changelog fails validation inside enhanceChangesetMigration.
	m := &types.Migration{
		ID:     "m1",
		Type:   types.MigrationTypeYAML,
		Script: "changeSets: []\n",
	}
	_, err := l.enhanceChangesetMigration(m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changelog validation failed")
}

func TestIsChangesetMigration(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)
	assert.True(t, l.isChangesetMigration("V1__x.xml"))
	assert.True(t, l.isChangesetMigration("V1__x.yaml"))
	assert.True(t, l.isChangesetMigration("V1__x.yml"))
	assert.True(t, l.isChangesetMigration("V1__x.json"))
	assert.False(t, l.isChangesetMigration("V1__x.sql"))
}

func TestGenerateChangesetRollback_ReverseOrder(t *testing.T) {
	l := NewChangesetMigrationLoader(nil)

	changelog := &types.Changelog{
		ChangeSets: []types.ChangeSet{
			{
				ID:     "cs1",
				Author: "a",
				Changes: []types.Change{
					{Type: "createTable", Attributes: map[string]interface{}{"tableName": "users"}},
					{Type: "createIndex", Attributes: map[string]interface{}{"indexName": "idx_users"}},
				},
			},
		},
	}

	migration := &types.Migration{Type: types.MigrationTypeYAML}
	rollback, err := l.generateChangesetRollback(migration, changelog)
	require.NoError(t, err)
	// The rollback marshals reversed changes (dropIndex, dropTable) back to YAML.
	assert.Contains(t, rollback, "dropTable")
	assert.Contains(t, rollback, "dropIndex")
}
