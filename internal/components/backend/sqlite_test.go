/**
 * @file sqlite_test.go
 * @description End-to-end tests for the SQLite DatabaseBackend against a real
 *              temp-file SQLite database (connect, exec, query, transactions,
 *              schema/table/index introspection, migration tables, backup, SQL
 *              adaptation and validation).
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/backend/sqlite_test.go
 */

package backend

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// newConnectedSQLiteBackend opens a fresh SQLite backend against a temp-file
// database and connects it. A temp file (rather than :memory:) is used so that
// file-based operations like GetDatabaseInfo and CreateBackup exercise real
// on-disk behavior. The connection is closed via t.Cleanup.
func newConnectedSQLiteBackend(t *testing.T) (*SQLiteBackend, types.DatabaseConfig) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	cfg := types.DatabaseConfig{Database: dbPath}

	be, err := NewSQLiteBackend(cfg)
	require.NoError(t, err)

	sqliteBE, ok := be.(*SQLiteBackend)
	require.True(t, ok, "NewSQLiteBackend must return *SQLiteBackend")

	require.NoError(t, sqliteBE.Connect(context.Background(), cfg))
	t.Cleanup(func() {
		_ = sqliteBE.Disconnect(context.Background())
	})

	return sqliteBE, cfg
}

func TestSQLiteBackend_ConnectPingDisconnect(t *testing.T) {
	be, cfg := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	assert.True(t, be.IsConnected())
	assert.Equal(t, DialectSQLite, be.GetDialect())
	require.NoError(t, be.Ping(ctx))

	// Connect is idempotent while already connected.
	require.NoError(t, be.Connect(ctx, cfg))
	assert.True(t, be.IsConnected())

	require.NoError(t, be.Disconnect(ctx))
	assert.False(t, be.IsConnected())

	// Ping after disconnect must error and Disconnect must be idempotent.
	assert.Error(t, be.Ping(ctx))
	require.NoError(t, be.Disconnect(ctx))
}

func TestSQLiteBackend_OperationsBeforeConnectFail(t *testing.T) {
	ctx := context.Background()
	be, err := NewSQLiteBackend(types.DatabaseConfig{Database: filepath.Join(t.TempDir(), "x.db")})
	require.NoError(t, err)

	assert.False(t, be.IsConnected())

	_, err = be.Execute(ctx, "SELECT 1")
	assert.Error(t, err)

	_, err = be.Query(ctx, "SELECT 1")
	assert.Error(t, err)

	_, err = be.BeginTransaction(ctx)
	assert.Error(t, err)

	// QueryRow returns a row whose Scan surfaces the not-connected error.
	row := be.QueryRow(ctx, "SELECT 1")
	var v int
	assert.Error(t, row.Scan(&v))
}

func TestSQLiteBackend_ExecuteAndQueryRows(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	res, err := be.Execute(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`)
	require.NoError(t, err)
	require.NotNil(t, res)

	ins, err := be.Execute(ctx, `INSERT INTO users (name) VALUES (?)`, "alice")
	require.NoError(t, err)

	affected, err := ins.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	lastID, err := ins.LastInsertId()
	require.NoError(t, err)
	assert.Equal(t, int64(1), lastID)

	_, err = be.Execute(ctx, `INSERT INTO users (name) VALUES (?)`, "bob")
	require.NoError(t, err)

	// Query multiple rows and verify column metadata + values.
	rows, err := be.Query(ctx, `SELECT id, name FROM users ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	cols, err := rows.Columns()
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name"}, cols)

	colTypes, err := rows.ColumnTypes()
	require.NoError(t, err)
	assert.Len(t, colTypes, 2)

	var names []string
	for rows.Next() {
		var id int64
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"alice", "bob"}, names)
}

func TestSQLiteBackend_QueryRowSingle(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)`)
	require.NoError(t, err)
	_, err = be.Execute(ctx, `INSERT INTO t (id, val) VALUES (1, 'hello')`)
	require.NoError(t, err)

	var val string
	require.NoError(t, be.QueryRow(ctx, `SELECT val FROM t WHERE id = ?`, 1).Scan(&val))
	assert.Equal(t, "hello", val)

	// Missing row surfaces sql.ErrNoRows.
	var missing string
	err = be.QueryRow(ctx, `SELECT val FROM t WHERE id = ?`, 999).Scan(&missing)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestSQLiteBackend_TransactionCommit(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE acct (id INTEGER PRIMARY KEY, balance INTEGER)`)
	require.NoError(t, err)

	tx, err := be.BeginTransaction(ctx)
	require.NoError(t, err)

	_, err = tx.Execute(ctx, `INSERT INTO acct (id, balance) VALUES (1, 100)`)
	require.NoError(t, err)

	// Row is visible within the transaction.
	var bal int
	require.NoError(t, tx.QueryRow(ctx, `SELECT balance FROM acct WHERE id = 1`).Scan(&bal))
	assert.Equal(t, 100, bal)

	require.NoError(t, tx.Commit())

	// Row survives after commit.
	var persisted int
	require.NoError(t, be.QueryRow(ctx, `SELECT balance FROM acct WHERE id = 1`).Scan(&persisted))
	assert.Equal(t, 100, persisted)
}

func TestSQLiteBackend_TransactionRollback(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE tmp (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	tx, err := be.BeginTransaction(ctx)
	require.NoError(t, err)

	_, err = tx.Execute(ctx, `INSERT INTO tmp (id) VALUES (42)`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	// Rolled-back row must not persist.
	var count int
	require.NoError(t, be.QueryRow(ctx, `SELECT COUNT(*) FROM tmp`).Scan(&count))
	assert.Equal(t, 0, count)
}

func TestSQLiteBackend_TransactionQueryRows(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE nums (n INTEGER)`)
	require.NoError(t, err)

	tx, err := be.BeginTransactionWithOptions(ctx, TransactionOptions{})
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		_, err = tx.Execute(ctx, `INSERT INTO nums (n) VALUES (?)`, i)
		require.NoError(t, err)
	}

	rows, err := tx.Query(ctx, `SELECT n FROM nums ORDER BY n`)
	require.NoError(t, err)
	var sum int
	for rows.Next() {
		var n int
		require.NoError(t, rows.Scan(&n))
		sum += n
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, 6, sum)

	require.NoError(t, tx.Commit())
}

func TestSQLiteBackend_Savepoints(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE sp (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	tx, err := be.BeginTransaction(ctx)
	require.NoError(t, err)

	_, err = tx.Execute(ctx, `INSERT INTO sp (id) VALUES (1)`)
	require.NoError(t, err)

	require.NoError(t, tx.Savepoint("sp1"))
	_, err = tx.Execute(ctx, `INSERT INTO sp (id) VALUES (2)`)
	require.NoError(t, err)

	// Roll back to the savepoint: id=2 is undone, id=1 remains.
	require.NoError(t, tx.RollbackToSavepoint("sp1"))
	require.NoError(t, tx.ReleaseSavepoint("sp1"))
	require.NoError(t, tx.Commit())

	var count int
	require.NoError(t, be.QueryRow(ctx, `SELECT COUNT(*) FROM sp`).Scan(&count))
	assert.Equal(t, 1, count)

	var id int
	require.NoError(t, be.QueryRow(ctx, `SELECT id FROM sp`).Scan(&id))
	assert.Equal(t, 1, id)
}

func TestSQLiteBackend_CreateTableViaDefinition(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	def := TableDefinition{
		Name: "products",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "INTEGER", Primary: true, AutoIncrement: true},
			{Name: "sku", Type: "TEXT", Nullable: false, Unique: true},
			{Name: "price", Type: "REAL", Nullable: false, DefaultValue: 0},
			{Name: "note", Type: "TEXT", Nullable: true},
		},
	}
	require.NoError(t, be.CreateTable(ctx, def))

	exists, err := be.TableExists(ctx, "products")
	require.NoError(t, err)
	assert.True(t, exists)

	// Unique + not-null constraints are enforced by the generated DDL.
	_, err = be.Execute(ctx, `INSERT INTO products (sku, price) VALUES ('A', 1.5)`)
	require.NoError(t, err)
	_, err = be.Execute(ctx, `INSERT INTO products (sku, price) VALUES ('A', 2.0)`)
	assert.Error(t, err, "duplicate sku must violate UNIQUE constraint")

	// GetTableInfo returns the columns with primary/nullable flags parsed.
	info, err := be.GetTableInfo(ctx, "products")
	require.NoError(t, err)
	assert.Equal(t, "products", info.Name)
	require.Len(t, info.Columns, 4)

	byName := map[string]ColumnInfo{}
	for _, c := range info.Columns {
		byName[c.Name] = c
	}
	assert.True(t, byName["id"].Primary)
	assert.False(t, byName["sku"].Nullable)
	assert.True(t, byName["note"].Nullable)
}

func TestSQLiteBackend_CreateTableWithConstraints(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	// Parent table for the foreign-key reference.
	require.NoError(t, be.CreateTable(ctx, TableDefinition{
		Name: "authors",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "INTEGER", Primary: true, AutoIncrement: true},
		},
	}))

	def := TableDefinition{
		Name: "books",
		Columns: []ColumnDefinition{
			{Name: "id", Type: "INTEGER"},
			{Name: "author_id", Type: "INTEGER"},
			{Name: "title", Type: "TEXT"},
		},
		Constraints: []ConstraintDefinition{
			{Type: ConstraintPrimaryKey, Columns: []string{"id"}},
			{
				Type:              ConstraintForeignKey,
				Columns:           []string{"author_id"},
				ReferencedTable:   "authors",
				ReferencedColumns: []string{"id"},
				OnDelete:          ActionCascade,
			},
			{Name: "uq_title", Type: ConstraintUnique, Columns: []string{"title"}},
			{Type: ConstraintCheck, CheckExpression: "length(title) > 0"},
		},
	}
	require.NoError(t, be.CreateTable(ctx, def))

	exists, err := be.TableExists(ctx, "books")
	require.NoError(t, err)
	assert.True(t, exists)

	// CHECK constraint rejects an empty title.
	_, err = be.Execute(ctx, `INSERT INTO books (id, author_id, title) VALUES (1, NULL, '')`)
	assert.Error(t, err)

	// Foreign key with a non-existent author is rejected (pragmas enable FKs).
	_, err = be.Execute(ctx, `INSERT INTO books (id, author_id, title) VALUES (2, 999, 'x')`)
	assert.Error(t, err)
}

func TestSQLiteBackend_DropTableAndListTables(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE a (id INTEGER)`)
	require.NoError(t, err)
	_, err = be.Execute(ctx, `CREATE TABLE b (id INTEGER)`)
	require.NoError(t, err)

	tables, err := be.ListTables(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, tables)

	require.NoError(t, be.DropTable(ctx, "a"))

	exists, err := be.TableExists(ctx, "a")
	require.NoError(t, err)
	assert.False(t, exists)

	tables, err = be.ListTables(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, tables)

	// DropTable is idempotent (IF EXISTS).
	require.NoError(t, be.DropTable(ctx, "a"))
}

func TestSQLiteBackend_Indexes(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE items (id INTEGER, category TEXT, code TEXT)`)
	require.NoError(t, err)

	require.NoError(t, be.CreateIndex(ctx, IndexDefinition{
		Name:    "idx_category",
		Table:   "items",
		Columns: []string{"category"},
	}))

	require.NoError(t, be.CreateIndex(ctx, IndexDefinition{
		Name:    "idx_code_unique",
		Table:   "items",
		Columns: []string{"code"},
		Unique:  true,
	}))

	exists, err := be.IndexExists(ctx, "idx_category")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = be.IndexExists(ctx, "does_not_exist")
	require.NoError(t, err)
	assert.False(t, exists)

	indexes, err := be.ListIndexes(ctx, "items")
	require.NoError(t, err)
	names := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		names = append(names, idx.Name)
		assert.Equal(t, "items", idx.Table)
	}
	assert.ElementsMatch(t, []string{"idx_category", "idx_code_unique"}, names)

	// The unique index enforces uniqueness on code.
	_, err = be.Execute(ctx, `INSERT INTO items (id, category, code) VALUES (1, 'x', 'C1')`)
	require.NoError(t, err)
	_, err = be.Execute(ctx, `INSERT INTO items (id, category, code) VALUES (2, 'x', 'C1')`)
	assert.Error(t, err)

	require.NoError(t, be.DropIndex(ctx, "idx_category"))
	exists, err = be.IndexExists(ctx, "idx_category")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestSQLiteBackend_Schemas(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	// Built-in schemas exist; CreateSchema/DropSchema are no-ops in SQLite.
	require.NoError(t, be.CreateSchema(ctx, "main"))
	require.NoError(t, be.CreateSchema(ctx, "custom"))
	require.NoError(t, be.DropSchema(ctx, "custom"))

	ok, err := be.SchemaExists(ctx, "main")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = be.SchemaExists(ctx, "temp")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = be.SchemaExists(ctx, "nonexistent_schema")
	require.NoError(t, err)
	assert.False(t, ok)

	schemas, err := be.ListSchemas(ctx)
	require.NoError(t, err)
	assert.Contains(t, schemas, "main")
}

func TestSQLiteBackend_ConstraintOperations(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	// SQLite cannot add/drop constraints after creation.
	err := be.AddConstraint(ctx, ConstraintDefinition{Type: ConstraintUnique, Table: "t", Columns: []string{"c"}})
	assert.Error(t, err)

	err = be.DropConstraint(ctx, "t", "some_constraint")
	assert.Error(t, err)

	// ListConstraints returns an empty (non-nil-behaving) slice without error.
	constraints, err := be.ListConstraints(ctx, "t")
	require.NoError(t, err)
	assert.Empty(t, constraints)
}

func TestSQLiteBackend_MigrationAndLockTables(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	require.NoError(t, be.CreateMigrationTable(ctx, "schema_migrations"))
	require.NoError(t, be.CreateLockTable(ctx, "schema_migrations_lock"))

	// Both tables are created and re-runnable (IF NOT EXISTS).
	require.NoError(t, be.CreateMigrationTable(ctx, "schema_migrations"))

	exists, err := be.TableExists(ctx, "schema_migrations")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = be.TableExists(ctx, "schema_migrations_lock")
	require.NoError(t, err)
	assert.True(t, exists)

	// The migration table tracks version + checksum as designed.
	_, err = be.Execute(ctx,
		`INSERT INTO schema_migrations (version, description, checksum) VALUES (?, ?, ?)`,
		"V1", "initial", "abc123")
	require.NoError(t, err)

	var version, checksum string
	var success bool
	require.NoError(t, be.QueryRow(ctx,
		`SELECT version, checksum, success FROM schema_migrations WHERE version = ?`, "V1").
		Scan(&version, &checksum, &success))
	assert.Equal(t, "V1", version)
	assert.Equal(t, "abc123", checksum)
	assert.True(t, success, "success defaults to 1/true")

	// version is UNIQUE: a duplicate insert must fail.
	_, err = be.Execute(ctx,
		`INSERT INTO schema_migrations (version) VALUES (?)`, "V1")
	assert.Error(t, err)
}

func TestSQLiteBackend_DatabaseInfoAndVersion(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	// Write something so the file has non-zero size.
	_, err := be.Execute(ctx, `CREATE TABLE sizer (id INTEGER PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	version, err := be.GetVersion(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, version)

	info, err := be.GetDatabaseInfo(ctx)
	require.NoError(t, err)
	assert.Equal(t, DialectSQLite, info.Dialect)
	assert.Equal(t, version, info.Version)
	assert.Equal(t, "test.db", info.Name)
}

func TestSQLiteBackend_CreateBackup(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)
	ctx := context.Background()

	_, err := be.Execute(ctx, `CREATE TABLE data (id INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = be.Execute(ctx, `INSERT INTO data (v) VALUES ('payload')`)
	require.NoError(t, err)

	// Empty backup path is rejected.
	assert.Error(t, be.CreateBackup(ctx, BackupConfig{}))

	backupPath := filepath.Join(t.TempDir(), "backup", "snap.db")
	require.NoError(t, be.CreateBackup(ctx, BackupConfig{Path: backupPath}))

	// The backup is a valid SQLite database containing the row.
	verify, err := NewSQLiteBackend(types.DatabaseConfig{Database: backupPath})
	require.NoError(t, err)
	require.NoError(t, verify.Connect(ctx, types.DatabaseConfig{Database: backupPath}))
	defer verify.Disconnect(ctx)

	var v string
	require.NoError(t, verify.QueryRow(ctx, `SELECT v FROM data WHERE id = 1`).Scan(&v))
	assert.Equal(t, "payload", v)
}

func TestSQLiteBackend_RestoreBackup(t *testing.T) {
	ctx := context.Background()

	// Build a source database on disk.
	primaryPath := filepath.Join(t.TempDir(), "primary.db")
	cfg := types.DatabaseConfig{Database: primaryPath}
	be, err := NewSQLiteBackend(cfg)
	require.NoError(t, err)
	primary := be.(*SQLiteBackend)
	require.NoError(t, primary.Connect(ctx, cfg))
	t.Cleanup(func() { _ = primary.Disconnect(ctx) })

	_, err = primary.Execute(ctx, `CREATE TABLE kv (k TEXT PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)
	_, err = primary.Execute(ctx, `INSERT INTO kv (k, v) VALUES ('one', 'first')`)
	require.NoError(t, err)

	// Take a backup, then mutate the live DB.
	backupPath := filepath.Join(t.TempDir(), "restore-src.db")
	require.NoError(t, primary.CreateBackup(ctx, BackupConfig{Path: backupPath}))

	_, err = primary.Execute(ctx, `UPDATE kv SET v = 'CHANGED' WHERE k = 'one'`)
	require.NoError(t, err)

	// Restore rolls the database back to the backup contents and reconnects.
	require.NoError(t, primary.RestoreBackup(ctx, RestoreConfig{Path: backupPath}))
	assert.True(t, primary.IsConnected())

	var v string
	require.NoError(t, primary.QueryRow(ctx, `SELECT v FROM kv WHERE k = 'one'`).Scan(&v))
	assert.Equal(t, "first", v)

	// Missing restore source and empty path are rejected.
	assert.Error(t, primary.RestoreBackup(ctx, RestoreConfig{}))
	assert.Error(t, primary.RestoreBackup(ctx, RestoreConfig{Path: filepath.Join(t.TempDir(), "nope.db")}))
}

func TestSQLiteBackend_AdaptSQL(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"backtick to double quote", "SELECT `id` FROM `t`", `SELECT "id" FROM "t"`},
		{"auto increment", "id INT AUTO_INCREMENT", "id INT AUTOINCREMENT"},
		{"now to datetime", "SELECT NOW()", "SELECT datetime('now')"},
		{"current_timestamp", "DEFAULT CURRENT_TIMESTAMP", "DEFAULT datetime('now')"},
		{"fetch first to limit", "SELECT * FROM t FETCH FIRST 10 ROWS ONLY", "SELECT * FROM t LIMIT 10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, be.AdaptSQL(tc.in))
		})
	}
}

func TestSQLiteBackend_ValidateSQL(t *testing.T) {
	be, _ := newConnectedSQLiteBackend(t)

	// Safe statements pass.
	require.NoError(t, be.ValidateSQL("SELECT * FROM users"))
	require.NoError(t, be.ValidateSQL("INSERT INTO users (name) VALUES ('x')"))

	// Dangerous statements are rejected.
	assert.Error(t, be.ValidateSQL("DROP TABLE users"))
	assert.Error(t, be.ValidateSQL("VACUUM"))
}

func TestSQLiteBackend_BuildDBPath(t *testing.T) {
	be := &SQLiteBackend{}

	assert.Equal(t, ":memory:", be.buildDBPath(types.DatabaseConfig{}))
	assert.Equal(t, "mydb.db", be.buildDBPath(types.DatabaseConfig{Database: "mydb"}))
	assert.Equal(t, "data.sqlite", be.buildDBPath(types.DatabaseConfig{Database: "data.sqlite"}))

	abs := filepath.Join(t.TempDir(), "abs.db")
	assert.Equal(t, abs, be.buildDBPath(types.DatabaseConfig{Database: abs}))
}

func TestSQLiteBackend_InMemory(t *testing.T) {
	ctx := context.Background()

	// An empty Database config resolves to an in-memory SQLite database.
	be, err := NewSQLiteBackend(types.DatabaseConfig{})
	require.NoError(t, err)
	sqliteBE := be.(*SQLiteBackend)
	require.NoError(t, sqliteBE.Connect(ctx, types.DatabaseConfig{}))
	t.Cleanup(func() { _ = sqliteBE.Disconnect(ctx) })

	assert.True(t, sqliteBE.IsConnected())

	_, err = sqliteBE.Execute(ctx, `CREATE TABLE mem (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	exists, err := sqliteBE.TableExists(ctx, "mem")
	require.NoError(t, err)
	assert.True(t, exists)
}
