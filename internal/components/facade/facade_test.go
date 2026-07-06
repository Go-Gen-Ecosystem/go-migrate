/**
 * @file facade_test.go
 * @description Behaviour tests for the facade layer. The database facade is
 *              driven against a real in-memory SQLite database (sqlite3 driver):
 *              it connects, creates the migration tracking table, executes real
 *              statements and inspects the resulting schema. The configuration,
 *              command, CLI and migration facades are exercised through their
 *              public contracts.
 * @author Admilson B. F. Cossa
 */

package facade

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqliteDBConfig returns a DatabaseConfig pointing at a private in-memory
// SQLite database using the sqlite3 database/sql driver.
func sqliteDBConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Driver:         "sqlite3",
		Database:       ":memory:",
		ConnectTimeout: 5 * time.Second,
		QueryTimeout:   5 * time.Second,
		MaxConnections: 1,
		MaxIdleConns:   1,
	}
}

// --- DefaultDatabaseFacade: real SQLite behaviour ----------------------------------

func TestDatabaseFacade_ConnectAndPing(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)

	require.NoError(t, f.Connect(ctx))
	assert.True(t, f.IsConnected(ctx))

	// Idempotent connect.
	require.NoError(t, f.Connect(ctx))

	require.NoError(t, f.Disconnect(ctx))
	assert.False(t, f.IsConnected(ctx))
}

func TestDatabaseFacade_NilConfig(t *testing.T) {
	_, err := NewDatabaseFacade(nil)
	require.Error(t, err)
}

func TestDatabaseFacade_CreateMigrationTableAndExists(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	require.NoError(t, f.CreateMigrationTable(ctx, "schema_migrations"))

	exists, err := f.TableExists(ctx, "main", "schema_migrations")
	require.NoError(t, err)
	assert.True(t, exists, "migration table must exist after creation")

	missing, err := f.TableExists(ctx, "main", "no_such_table")
	require.NoError(t, err)
	assert.False(t, missing)
}

func TestDatabaseFacade_ExecuteStatementAndQuery(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteStatement(ctx, "CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT)")
	require.NoError(t, err)

	res, err := f.ExecuteStatement(ctx, "INSERT INTO people (name) VALUES ('ada')")
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.RowsAffected)

	q, err := f.ExecuteQuery(ctx, "SELECT id, name FROM people")
	require.NoError(t, err)
	require.Equal(t, 1, q.RowCount)
	assert.Equal(t, []string{"id", "name"}, q.Columns)
}

func TestDatabaseFacade_GetTablesAndSchema(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteStatement(ctx, "CREATE TABLE t1 (id INT)")
	require.NoError(t, err)

	tables, err := f.GetTables(ctx, "main")
	require.NoError(t, err)
	var names []string
	for _, tbl := range tables {
		names = append(names, tbl.Name)
	}
	assert.Contains(t, names, "t1")

	schema, err := f.GetCurrentSchema(ctx)
	require.NoError(t, err)
	assert.Equal(t, "main", schema)

	version, err := f.GetDatabaseVersion(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, version)

	info, err := f.GetDatabaseInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, "sqlite3", info.Driver)
}

func TestDatabaseFacade_Transaction(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteStatement(ctx, "CREATE TABLE tx (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	tx, err := f.BeginTransaction(ctx, &TransactionOptions{})
	require.NoError(t, err)
	_, err = tx.ExecuteStatement(ctx, "INSERT INTO tx (id) VALUES (1)")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	q, err := f.ExecuteQuery(ctx, "SELECT count(*) FROM tx")
	require.NoError(t, err)
	assert.Equal(t, 1, q.RowCount)
}

func TestDatabaseFacade_NotConnectedErrors(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)

	_, err = f.ExecuteQuery(ctx, "SELECT 1")
	assert.Error(t, err)
	_, err = f.ExecuteStatement(ctx, "SELECT 1")
	assert.Error(t, err)
	assert.False(t, f.IsConnected(ctx))
}

func TestDatabaseFacade_GetConnection(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	defer f.Disconnect(ctx)

	// GetConnection connects lazily on first use.
	conn, err := f.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.NoError(t, conn.PingContext(ctx))
}

func TestDatabaseFacade_GetSchemas_SQLite(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	// SQLite has no schemas; the facade returns an empty list without error.
	schemas, err := f.GetSchemas(ctx)
	require.NoError(t, err)
	assert.Empty(t, schemas)
}

func TestDatabaseFacade_TransactionRollback(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteStatement(ctx, "CREATE TABLE rb (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	tx, err := f.BeginTransaction(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecuteStatement(ctx, "INSERT INTO rb (id) VALUES (1)")
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	// The insert was rolled back.
	q, err := f.ExecuteQuery(ctx, "SELECT count(*) AS n FROM rb")
	require.NoError(t, err)
	require.Equal(t, 1, q.RowCount)
}

func TestDatabaseFacade_ExecuteQuery_BadSQL(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteQuery(ctx, "SELECT * FROM nonexistent_table")
	assert.Error(t, err)
}

func TestDatabaseFacade_TransactionQueryWithIsolation(t *testing.T) {
	ctx := context.Background()
	f, err := NewDatabaseFacade(sqliteDBConfig())
	require.NoError(t, err)
	require.NoError(t, f.Connect(ctx))
	defer f.Disconnect(ctx)

	_, err = f.ExecuteStatement(ctx, "CREATE TABLE iso (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	_, err = f.ExecuteStatement(ctx, "INSERT INTO iso (id) VALUES (7)")
	require.NoError(t, err)

	// Exercise convertIsolationLevel through a serializable transaction.
	tx, err := f.BeginTransaction(ctx, &TransactionOptions{IsolationLevel: "SERIALIZABLE", ReadOnly: true})
	require.NoError(t, err)
	rows, err := tx.ExecuteQuery(ctx, "SELECT id FROM iso")
	require.NoError(t, err)
	defer rows.Close()
	require.True(t, rows.Next())
	var id int
	require.NoError(t, rows.Scan(&id))
	assert.Equal(t, 7, id)
	require.NoError(t, tx.Commit())
}

// TestDatabaseFacade_ConnectionStringBuilders drives Connect for the postgres
// and mysql drivers far enough to build their connection strings. The actual
// sql.Open fails because those drivers are not registered, which is expected.
func TestDatabaseFacade_ConnectionStringBuilders(t *testing.T) {
	ctx := context.Background()

	pg := &DatabaseConfig{
		Driver: "postgres", Host: "localhost", Port: 5432, Database: "db",
		Username: "u", Password: "p", SSLMode: "disable",
		ConnectTimeout: time.Second, Parameters: map[string]string{"application_name": "gm"},
	}
	pf, err := NewDatabaseFacade(pg)
	require.NoError(t, err)
	assert.Error(t, pf.Connect(ctx)) // unregistered driver, but the DSN was built

	my := &DatabaseConfig{
		Driver: "mysql", Host: "localhost", Port: 3306, Database: "db",
		Username: "u", Password: "p", ConnectTimeout: time.Second,
		Parameters: map[string]string{"charset": "utf8mb4"},
	}
	mf, err := NewDatabaseFacade(my)
	require.NoError(t, err)
	assert.Error(t, mf.Connect(ctx))
}

// --- DefaultConfigurationFacade -----------------------------------------------------

func newLoadedConfigFacade(t *testing.T) *DefaultConfigurationFacade {
	t.Helper()
	f, err := NewDefaultConfigurationFacade(nil)
	require.NoError(t, err)
	require.NoError(t, f.LoadConfiguration(context.Background()))
	return f
}

func TestConfigurationFacade_SetGetTypedValues(t *testing.T) {
	f := newLoadedConfigFacade(t)

	require.NoError(t, f.SetValue("database.host", "localhost"))
	require.NoError(t, f.SetValue("database.port", 5432))
	require.NoError(t, f.SetValue("features.enabled", true))
	require.NoError(t, f.SetValue("timeout", "30s"))

	host, err := f.GetString("database.host")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host)

	port, err := f.GetInt("database.port")
	require.NoError(t, err)
	assert.Equal(t, 5432, port)

	enabled, err := f.GetBool("features.enabled")
	require.NoError(t, err)
	assert.True(t, enabled)

	d, err := f.GetDuration("timeout")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}

func TestConfigurationFacade_GetValue_NotFound(t *testing.T) {
	f := newLoadedConfigFacade(t)
	_, err := f.GetValue("does.not.exist")
	assert.Error(t, err)
}

func TestConfigurationFacade_NotLoaded(t *testing.T) {
	f, err := NewDefaultConfigurationFacade(nil)
	require.NoError(t, err)
	// Without LoadConfiguration, reads and writes are rejected.
	_, err = f.GetValue("any")
	assert.Error(t, err)
	assert.Error(t, f.SetValue("any", 1))
}

func TestConfigurationFacade_HasKeyAndAllKeys(t *testing.T) {
	f := newLoadedConfigFacade(t)
	require.NoError(t, f.SetValue("service.name", "migrate"))

	assert.True(t, f.HasKey("service.name"))
	assert.False(t, f.HasKey("service.missing"))

	keys := f.GetAllKeys()
	assert.Contains(t, keys, "service.name")
}

func TestConfigurationFacade_Info(t *testing.T) {
	f := newLoadedConfigFacade(t)
	require.NoError(t, f.SetValue("k", "v"))
	info, err := f.GetConfigurationInfo()
	require.NoError(t, err)
	assert.True(t, info.Loaded)
	assert.GreaterOrEqual(t, info.KeyCount, 1)
}

// --- DefaultCommandFacade -----------------------------------------------------------

func TestCommandFacade_RegisterExecuteGet(t *testing.T) {
	cf := NewDefaultCommandFacade()

	require.NoError(t, cf.RegisterCommand(&VersionCommand{}))
	require.NoError(t, cf.RegisterCommand(&HelpCommand{}))

	// Registered commands are discoverable.
	names := make(map[string]bool)
	for _, info := range cf.GetCommands() {
		names[info.Name] = true
	}
	assert.True(t, names["version"])
	assert.True(t, names["help"])

	got, err := cf.GetCommand("version")
	require.NoError(t, err)
	assert.Equal(t, "version", got.GetInfo().Name)

	_, err = cf.GetCommand("missing")
	assert.Error(t, err)

	// Execute a self-contained command.
	res, err := cf.ExecuteCommand(context.Background(), "version", nil, &ExecutionOptions{})
	require.NoError(t, err)
	assert.True(t, res.Success)

	_, err = cf.ExecuteCommand(context.Background(), "missing", nil, &ExecutionOptions{})
	assert.Error(t, err)
}

// --- DefaultCLIFacade ---------------------------------------------------------------

func TestCLIFacade_ParseCommand(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)

	cmd, err := cli.ParseCommand([]string{"up", "v1.0.0", "--dry-run", "--verbose"})
	require.NoError(t, err)
	assert.Equal(t, "up", cmd.Name)
	require.Len(t, cmd.Args, 1)
	assert.Equal(t, "v1.0.0", cmd.Args[0])
	assert.Equal(t, "true", cmd.Flags["dry-run"])
	assert.Equal(t, "true", cmd.Flags["verbose"])
}

func TestCLIFacade_ParseCommand_Empty(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	_, err := cli.ParseCommand(nil)
	assert.Error(t, err)
}

func TestCLIFacade_ValidateCommand(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)

	assert.Error(t, cli.ValidateCommand(nil))
	assert.Error(t, cli.ValidateCommand(&Command{Name: ""}))
	assert.Error(t, cli.ValidateCommand(&Command{Name: "bogus"}))
	require.NoError(t, cli.ValidateCommand(&Command{Name: "version"}))
}

func TestCLIFacade_ListCommands(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	cmds := cli.ListCommands()
	require.NotEmpty(t, cmds)
	names := make(map[string]bool)
	for _, c := range cmds {
		names[c.Name] = true
	}
	assert.True(t, names["version"])
	assert.True(t, names["help"])
}

func TestCLIFacade_ExecuteCommand_Version(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	cmd, err := cli.ParseCommand([]string{"version"})
	require.NoError(t, err)

	res, err := cli.ExecuteCommand(context.Background(), cmd)
	require.NoError(t, err)
	assert.True(t, res.Success)
	assert.Equal(t, 0, res.ExitCode)
}

func TestCLIFacade_ExecuteCommand_Unknown(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)
	res, err := cli.ExecuteCommand(context.Background(), &Command{Name: "nope"})
	require.Error(t, err)
	assert.False(t, res.Success)
}

func TestCLIFacade_GetHelpAndUsage(t *testing.T) {
	cli := NewDefaultCLIFacade(nil, nil)

	help, err := cli.GetHelp("version")
	require.NoError(t, err)
	assert.Equal(t, "version", help.Command)

	_, err = cli.GetHelp("missing")
	assert.Error(t, err)

	assert.Contains(t, cli.GetUsage(""), "migration-tool")
	assert.Contains(t, cli.GetUsage("version"), "version")
}

// --- DefaultMigrationFacade (panic-free query surface) ------------------------------

func newMigrationFacade(t *testing.T) *DefaultMigrationFacade {
	t.Helper()
	cfg := DefaultMigrationFacadeConfig()
	cfg.DatabaseConfig = sqliteDBConfig()
	f, err := NewDefaultMigrationFacade(cfg)
	require.NoError(t, err)
	return f
}

func TestMigrationFacade_List_Empty(t *testing.T) {
	f := newMigrationFacade(t)
	res, err := f.List(context.Background(), &ListOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 0, res.TotalCount)
	require.NotNil(t, res.Summary)
	assert.Equal(t, 0, res.Summary.TotalMigrations)
}

func TestMigrationFacade_History_Empty(t *testing.T) {
	f := newMigrationFacade(t)
	res, err := f.History(context.Background(), &HistoryOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 0, res.TotalCount)
	assert.Equal(t, 0, res.Summary.TotalOperations)
}

func TestMigrationFacade_Validate_Empty(t *testing.T) {
	f := newMigrationFacade(t)
	res, err := f.Validate(context.Background(), &ValidateOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsValid)
	assert.Equal(t, 0, res.CheckedCount)
}

func TestMigrationFacade_Version(t *testing.T) {
	f := newMigrationFacade(t)
	info, err := f.Version(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	// Environment seeds the schema version fallback.
	assert.NotEmpty(t, info.SchemaVersion)
}

func TestMigrationFacade_NilConfigUsesDefaults(t *testing.T) {
	// With no DatabaseConfig, initialization fails because the database facade
	// requires one.
	_, err := NewDefaultMigrationFacade(nil)
	require.Error(t, err)
}

// --- FacadeFactory ------------------------------------------------------------------

func TestFacadeFactory_Construction(t *testing.T) {
	f, err := NewFacadeFactory(nil)
	require.NoError(t, err)
	require.NotNil(t, f)

	// Before Initialize, getters report the not-initialized error.
	_, err = f.GetMigrationFacade()
	assert.Error(t, err)
}

func TestFacadeFactory_ConfigHelpers(t *testing.T) {
	assert.Equal(t, "production", ProductionFactoryConfig().Environment)
	assert.Equal(t, "development", DevelopmentFactoryConfig().Environment)
	assert.Equal(t, "testing", TestingFactoryConfig().Environment)

	def := DefaultFactoryConfig()
	require.NotNil(t, def)
	assert.True(t, def.EnableMigrationFacade)
	require.NotNil(t, DefaultCacheConfig())
	require.NotNil(t, DefaultSecurityConfig())
	require.NotNil(t, DefaultPerformanceConfig())
}

// --- Default config constructors ----------------------------------------------------

func TestDefaultConfigs(t *testing.T) {
	mc := DefaultMigrationFacadeConfig()
	assert.Equal(t, "migrations", mc.MigrationsPath)
	assert.Equal(t, "development", mc.Environment)

	dc := DefaultDatabaseConfig()
	assert.Equal(t, "postgres", dc.Driver)

	cc := DefaultConfigurationConfig()
	assert.Equal(t, "development", cc.Environment)
}

// --- SeedFile round-trip through the migration facade (real file IO) ----------------

func TestMigrationFacade_Seed_ReadsAndExecutes(t *testing.T) {
	f := newMigrationFacade(t)
	ctx := context.Background()

	// Connect the underlying database facade so seed statements can execute.
	require.NoError(t, f.databaseFacade.Connect(ctx))
	defer f.databaseFacade.Disconnect(ctx)

	dir := t.TempDir()
	seed := filepath.Join(dir, "seed.sql")
	require.NoError(t, os.WriteFile(seed, []byte("CREATE TABLE seeded (id INTEGER PRIMARY KEY)"), 0o644))

	res, err := f.Seed(ctx, &SeedOptions{Files: []string{"seed.sql"}, Directory: dir})
	require.NoError(t, err)
	assert.True(t, res.Success)
	require.Len(t, res.ExecutedFiles, 1)

	// The seed statement really ran against the database.
	exists, err := f.databaseFacade.TableExists(ctx, "main", "seeded")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestMigrationFacade_Seed_MissingFile(t *testing.T) {
	f := newMigrationFacade(t)
	ctx := context.Background()
	require.NoError(t, f.databaseFacade.Connect(ctx))
	defer f.databaseFacade.Disconnect(ctx)

	res, err := f.Seed(ctx, &SeedOptions{Files: []string{"absent.sql"}, Directory: t.TempDir()})
	require.NoError(t, err)
	assert.False(t, res.Success)
	require.Len(t, res.FailedFiles, 1)
}
