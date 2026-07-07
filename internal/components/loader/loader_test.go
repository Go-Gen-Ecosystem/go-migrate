/**
 * @file loader_test.go
 * @description Behavioral tests for the file migration loader: filename parsing,
 * +migrate Up/Down splitting, SQL metadata extraction, directory loading with
 * version ordering, and per-format validators.
 * @author Admilson B. F. Cossa
 */

package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile is a helper that writes content to a file inside dir and returns
// the full path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

func TestParseMigrationFilename(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantVersion string
		wantDesc    string
		wantErr     bool
	}{
		{
			name:        "versioned V-style",
			filename:    "V1__create_users.sql",
			wantVersion: "1",
			wantDesc:    "create users",
		},
		{
			name:        "versioned V-style dotted",
			filename:    "V1.2__add_index.sql",
			wantVersion: "1.2",
			wantDesc:    "add index",
		},
		{
			name:        "timestamp style",
			filename:    "20240101120000_seed_data.sql",
			wantVersion: "20240101120000",
			wantDesc:    "seed data",
		},
		{
			name:        "simple numeric",
			filename:    "001_initial.sql",
			wantVersion: "001",
			wantDesc:    "initial",
		},
		{
			name:     "invalid format",
			filename: "not-a-migration.sql",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, desc, err := ParseMigrationFilename(tt.filename)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version)
			assert.Equal(t, tt.wantDesc, desc)
		})
	}
}

func TestCalculateChecksum(t *testing.T) {
	content := "CREATE TABLE users (id INTEGER);"
	got := CalculateChecksum(content)

	sum := sha256.Sum256([]byte(content))
	want := hex.EncodeToString(sum[:])

	assert.Equal(t, want, got)
	// Deterministic: same content yields same checksum.
	assert.Equal(t, got, CalculateChecksum(content))
	// Different content yields a different checksum.
	assert.NotEqual(t, got, CalculateChecksum(content+" "))
}

func TestGenerateMigrationID(t *testing.T) {
	id1 := GenerateMigrationID("V1__a.sql")
	id2 := GenerateMigrationID("V1__a.sql")
	id3 := GenerateMigrationID("V2__b.sql")

	assert.Equal(t, id1, id2, "same filename must produce same ID")
	assert.NotEqual(t, id1, id3, "different filename must produce different ID")
	assert.Len(t, id1, 64, "SHA-256 hex digest is 64 chars")
}

func TestLoadMigration_SQLBasic(t *testing.T) {
	dir := t.TempDir()
	// SQL metadata comments use the "-- @key: value" colon form (the parser
	// splits on ':' after stripping the comment marker and leading '@').
	content := "-- @author: alice\n-- @depends_on: dep1,dep2\nCREATE TABLE users (id INTEGER PRIMARY KEY);"
	path := writeFile(t, dir, "V1__create_users.sql", content)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, types.MigrationTypeSQL, m.Type)
	assert.Equal(t, "1", m.Version)
	assert.Equal(t, "create users", m.Description)
	assert.Equal(t, "alice", m.Author)
	assert.Equal(t, []string{"dep1", "dep2"}, m.Dependencies)
	assert.Equal(t, path, m.FilePath)
	assert.NotEmpty(t, m.Checksum)
	assert.Equal(t, GenerateMigrationID("V1__create_users.sql"), m.ID)
	// Script is preserved for non +migrate files.
	assert.Contains(t, m.Script, "CREATE TABLE users")
}

func TestLoadMigration_MigrateUpDownFormat(t *testing.T) {
	dir := t.TempDir()
	content := "-- +migrate Up\nCREATE TABLE t (id INTEGER);\n-- +migrate Down\nDROP TABLE t;"
	path := writeFile(t, dir, "V2__with_rollback.sql", content)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "CREATE TABLE t (id INTEGER);", m.Script)
	assert.Equal(t, "DROP TABLE t;", m.RollbackSQL)
	assert.True(t, m.CanRollback)
}

func TestLoadMigration_RollbackMetadataComment(t *testing.T) {
	dir := t.TempDir()
	content := "-- @rollback_sql: DROP TABLE orders\nCREATE TABLE orders (id INTEGER);"
	path := writeFile(t, dir, "V3__orders.sql", content)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "DROP TABLE orders", m.RollbackSQL)
	assert.True(t, m.CanRollback)
}

func TestLoadMigration_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "notes.txt", "hello")

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}

func TestLoadMigration_MissingFile(t *testing.T) {
	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(filepath.Join(t.TempDir(), "does-not-exist.sql"))
	assert.Error(t, err)
}

func TestLoadMigration_MalformedFilename(t *testing.T) {
	dir := t.TempDir()
	// Valid extension but filename does not match any known version pattern.
	path := writeFile(t, dir, "random.sql", "CREATE TABLE x (id INTEGER);")

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid migration filename format")
}

func TestLoadMigrations_OrderingAndFiltering(t *testing.T) {
	dir := t.TempDir()
	// Intentionally out of numeric order; loader must sort ascending by version.
	writeFile(t, dir, "V10__ten.sql", "CREATE TABLE t10 (id INTEGER);")
	writeFile(t, dir, "V2__two.sql", "CREATE TABLE t2 (id INTEGER);")
	writeFile(t, dir, "V1__one.sql", "CREATE TABLE t1 (id INTEGER);")
	// Non-migration file that must be skipped entirely.
	writeFile(t, dir, "README.txt", "ignore me")

	loader := NewFileMigrationLoader()
	migrations, err := loader.LoadMigrations(dir)
	require.NoError(t, err)
	require.Len(t, migrations, 3)

	// Numeric sort: 1, 2, 10 (not lexicographic 1, 10, 2).
	assert.Equal(t, "1", migrations[0].Version)
	assert.Equal(t, "2", migrations[1].Version)
	assert.Equal(t, "10", migrations[2].Version)
}

func TestLoadMigrations_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	writeFile(t, dir, "V1__root.sql", "CREATE TABLE r (id INTEGER);")
	writeFile(t, sub, "V2__nested.sql", "CREATE TABLE n (id INTEGER);")

	loader := NewFileMigrationLoader()
	migrations, err := loader.LoadMigrations(dir)
	require.NoError(t, err)
	assert.Len(t, migrations, 2, "walk must descend into subdirectories")
}

func TestLoadMigrations_PropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	// Bad filename with a supported extension causes LoadMigration to fail,
	// which LoadMigrations must surface.
	writeFile(t, dir, "bad-name.sql", "CREATE TABLE x (id INTEGER);")

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigrations(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load migration")
}

func TestLoadMigrations_YAMLChangeset(t *testing.T) {
	dir := t.TempDir()
	// For YAML/JSON changesets a "sql" change type is converted from the
	// change's Content field (the "content" key), not the "sql" key, because
	// convertChangeToSQL reads Attributes["sql"] then falls back to Content.
	yamlContent := `changeSets:
  - id: cs-1
    author: bob
    comment: create widgets
    changes:
      - type: sql
        content: CREATE TABLE widgets (id INTEGER);
`
	writeFile(t, dir, "V1__widgets.yaml", yamlContent)

	loader := NewFileMigrationLoader()
	migrations, err := loader.LoadMigrations(dir)
	require.NoError(t, err)
	require.Len(t, migrations, 1)

	m := migrations[0]
	assert.Equal(t, types.MigrationTypeYAML, m.Type)
	assert.Equal(t, "cs-1", m.Version)
	assert.Equal(t, "bob", m.Author)
	assert.Equal(t, "create widgets", m.Description)
	assert.Contains(t, m.Script, "CREATE TABLE widgets")
}

func TestLoadMigration_JSONChangeset(t *testing.T) {
	dir := t.TempDir()
	jsonContent := `{
  "changeSets": [
    {
      "id": "json-cs",
      "author": "carol",
      "comment": "json migration",
      "changes": [
        {"type": "sql", "content": "CREATE TABLE things (id INTEGER);"}
      ]
    }
  ]
}`
	path := writeFile(t, dir, "V5__things.json", jsonContent)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, types.MigrationTypeJSON, m.Type)
	assert.Equal(t, "json-cs", m.Version)
	assert.Equal(t, "carol", m.Author)
	assert.Contains(t, m.Script, "CREATE TABLE things")
}

func TestLoadMigration_JSONNoChangesets(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "V6__empty.json", `{"changeSets": []}`)

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no changesets found")
}

func TestValidateMigration_SQL(t *testing.T) {
	loader := NewFileMigrationLoader()

	valid := &types.Migration{Type: types.MigrationTypeSQL, Script: "CREATE TABLE t (id INTEGER);"}
	assert.NoError(t, loader.ValidateMigration(valid))

	empty := &types.Migration{Type: types.MigrationTypeSQL, Script: "   "}
	assert.Error(t, loader.ValidateMigration(empty))

	unknown := &types.Migration{Type: types.MigrationType("bogus"), Script: "x"}
	err := loader.ValidateMigration(unknown)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no validator")
}

func TestSQLFileValidator(t *testing.T) {
	v := &SQLFileValidator{}

	assert.NoError(t, v.Validate("CREATE TABLE t (id INTEGER);"))
	assert.Error(t, v.Validate("   "), "empty content must fail")
	assert.Error(t, v.Validate("DROP DATABASE production;"), "dangerous op must fail")
	assert.Error(t, v.Validate("TRUNCATE TABLE users;"), "truncate must fail")
}

func TestSQLFileValidator_ExtractMetadata(t *testing.T) {
	v := &SQLFileValidator{}
	meta, err := v.ExtractMetadata("CREATE TABLE users (id INT);\nALTER TABLE orders ADD col INT;")
	require.NoError(t, err)

	tables, ok := meta["tables"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"users", "orders"}, tables)
}

func TestJSONFileValidator(t *testing.T) {
	v := &JSONFileValidator{}

	valid := `{"changeSets":[{"id":"a","author":"x","changes":[]}]}`
	assert.NoError(t, v.Validate(valid))
	assert.Error(t, v.Validate("{not json"))

	meta, err := v.ExtractMetadata(valid)
	require.NoError(t, err)
	assert.Equal(t, 1, meta["changesets"])
}

func TestYAMLFileValidator(t *testing.T) {
	v := &YAMLFileValidator{}

	valid := "changeSets:\n  - id: a\n    author: x\n    changes: []\n"
	assert.NoError(t, v.Validate(valid))
	assert.Error(t, v.Validate("::: not yaml :::\n\t- bad"))

	meta, err := v.ExtractMetadata(valid)
	require.NoError(t, err)
	assert.Equal(t, 1, meta["changesets"])
}

func TestXMLFileValidator(t *testing.T) {
	v := &XMLFileValidator{}

	valid := `<databaseChangeLog><changeSets id="a" author="x"></changeSets></databaseChangeLog>`
	assert.NoError(t, v.Validate(valid))
	assert.Error(t, v.Validate("<unclosed>"))

	meta, err := v.ExtractMetadata(valid)
	require.NoError(t, err)
	assert.Contains(t, meta, "changesets")
}

func TestLoadMigration_SQLAllMetadata(t *testing.T) {
	dir := t.TempDir()
	// Exercise the full metadata comment surface: author, contexts, labels.
	content := "-- @author: erin\n" +
		"-- @contexts: prod,staging\n" +
		"-- @labels: hotfix,urgent\n" +
		"CREATE TABLE audit (id INTEGER);"
	path := writeFile(t, dir, "V8__audit.sql", content)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "erin", m.Author)
	assert.Equal(t, []string{"prod", "staging"}, m.Contexts)
	assert.Equal(t, []string{"hotfix", "urgent"}, m.Labels)
}

func TestLoadMigration_MigrateUpOnly(t *testing.T) {
	dir := t.TempDir()
	// +migrate Up with no Down section: script is set, rollback stays empty.
	content := "-- +migrate Up\nCREATE TABLE only_up (id INTEGER);"
	path := writeFile(t, dir, "V9__only_up.sql", content)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "CREATE TABLE only_up (id INTEGER);", m.Script)
	assert.Empty(t, m.RollbackSQL)
	assert.False(t, m.CanRollback)
}

func TestLoadMigration_XMLWithRollbackAndContexts(t *testing.T) {
	dir := t.TempDir()
	// XML changeset with context/labels attributes and an explicit rollback.
	xmlContent := `<databaseChangeLog>
  <changeSets id="xml-rb" author="frank" comment="with rollback" context="prod,dev" labels="a,b">
    <changes type="sql"><![CDATA[CREATE TABLE t (id INTEGER);]]></changes>
    <rollback>DROP TABLE t;</rollback>
  </changeSets>
</databaseChangeLog>`
	path := writeFile(t, dir, "V11__xml_rb.xml", xmlContent)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "xml-rb", m.Version)
	assert.Equal(t, "frank", m.Author)
	assert.Equal(t, []string{"prod", "dev"}, m.Contexts)
	assert.Equal(t, []string{"a", "b"}, m.Labels)
	assert.Equal(t, "DROP TABLE t;", m.RollbackSQL)
}

func TestLoadMigration_YAMLNoChangesets(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "V12__empty.yaml", "changeSets: []\n")

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no changesets found")
}

func TestLoadMigration_XMLNoChangesets(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "V13__empty.xml", `<databaseChangeLog></databaseChangeLog>`)

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no changesets found")
}

func TestLoadMigration_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "V14__bad.yaml", "changeSets: [unclosed\n\t: :")

	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigration(path)
	assert.Error(t, err)
}

func TestLoadMigrations_EmptyDir(t *testing.T) {
	loader := NewFileMigrationLoader()
	migrations, err := loader.LoadMigrations(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, migrations)
}

func TestLoadMigrations_NonexistentPath(t *testing.T) {
	loader := NewFileMigrationLoader()
	_, err := loader.LoadMigrations(filepath.Join(t.TempDir(), "nope"))
	assert.Error(t, err)
}

func TestLoadMigration_YAMLStructuredChanges(t *testing.T) {
	dir := t.TempDir()
	// A createTable change with preconditions exercises the precondition
	// extraction path and confirms structured (non-sql) changes parse.
	yamlContent := `changeSets:
  - id: struct-1
    author: gale
    comment: structured
    runAlways: true
    failOnError: true
    preconditions:
      - onFail: HALT
        condition:
          tableExists:
            tableName: users
    changes:
      - type: createTable
        content: ""
`
	path := writeFile(t, dir, "V15__structured.yaml", yamlContent)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, "struct-1", m.Version)
	assert.True(t, m.RunAlways)
	assert.True(t, m.FailOnError)
	require.Len(t, m.Preconditions, 1)
	assert.Equal(t, "tableExists", m.Preconditions[0].Type)
	assert.Equal(t, "HALT", m.Preconditions[0].OnFail)
}

func TestLoadMigration_XMLChangeset(t *testing.T) {
	dir := t.TempDir()
	xmlContent := `<databaseChangeLog>
  <changeSets id="xml-1" author="dave" comment="xml change">
    <changes type="sql"><![CDATA[CREATE TABLE gadgets (id INTEGER);]]></changes>
  </changeSets>
</databaseChangeLog>`
	path := writeFile(t, dir, "V7__gadgets.xml", xmlContent)

	loader := NewFileMigrationLoader()
	m, err := loader.LoadMigration(path)
	require.NoError(t, err)

	assert.Equal(t, types.MigrationTypeXML, m.Type)
	assert.Equal(t, "xml-1", m.Version)
	assert.Equal(t, "dave", m.Author)
	assert.Equal(t, "xml change", m.Description)
}
