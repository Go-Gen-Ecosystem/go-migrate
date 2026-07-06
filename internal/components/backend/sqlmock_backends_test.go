/**
 * @file sqlmock_backends_test.go
 * @description Behavioral tests for the MySQL, PostgreSQL and SQL Server backends
 *              driven through go-sqlmock. A mocked *sql.DB is injected into each
 *              backend so the query/exec/transaction/DDL code paths run without a
 *              live server, asserting the generated SQL and result handling.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/backend/sqlmock_backends_test.go
 */

package backend

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// mockableBackend describes the fields shared by every SQL backend struct so a
// generic harness can inject a mocked *sql.DB and mark the backend connected.
type mockableBackend interface {
	DatabaseBackend
	setMock(db *sql.DB)
}

func (m *MySQLBackend) setMock(db *sql.DB)      { m.db = db; m.connected = true }
func (p *PostgreSQLBackend) setMock(db *sql.DB) { p.db = db; p.connected = true }
func (s *SQLServerBackend) setMock(db *sql.DB)  { s.db = db; s.connected = true }

// newMockBackend constructs a backend for the given constructor and wires it to a
// fresh sqlmock. QueryMatcherRegexp lets tests match on stable SQL fragments
// rather than reproducing each dialect's full statement verbatim.
func newMockBackend(t *testing.T, make func(types.DatabaseConfig) (DatabaseBackend, error)) (mockableBackend, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	be, err := make(types.DatabaseConfig{Database: "app"})
	require.NoError(t, err)

	mb, ok := be.(mockableBackend)
	require.True(t, ok)
	mb.setMock(db)

	return mb, mock
}

// backendCase describes a dialect under test and its expected dialect tag.
type backendCase struct {
	name    string
	make    func(types.DatabaseConfig) (DatabaseBackend, error)
	dialect DatabaseDialect
}

func sqlBackendCases() []backendCase {
	return []backendCase{
		{"mysql", NewMySQLBackend, DialectMySQL},
		{"postgresql", NewPostgreSQLBackend, DialectPostgreSQL},
		{"sqlserver", NewSQLServerBackend, DialectSQLServer},
	}
}

func TestSQLBackends_ExecuteSuccess(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectExec("INSERT INTO users").
				WithArgs("alice").
				WillReturnResult(sqlmock.NewResult(7, 1))

			res, err := be.Execute(context.Background(), "INSERT INTO users (name) VALUES (?)", "alice")
			require.NoError(t, err)

			affected, err := res.RowsAffected()
			require.NoError(t, err)
			assert.Equal(t, int64(1), affected)

			lastID, err := res.LastInsertId()
			require.NoError(t, err)
			assert.Equal(t, int64(7), lastID)

			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_ExecuteError(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectExec("BAD SQL").WillReturnError(sql.ErrConnDone)

			_, err := be.Execute(context.Background(), "BAD SQL")
			assert.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_QueryRows(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			rows := sqlmock.NewRows([]string{"id", "name"}).
				AddRow(1, "alice").
				AddRow(2, "bob")
			mock.ExpectQuery("SELECT id, name FROM users").WillReturnRows(rows)

			qr, err := be.Query(context.Background(), "SELECT id, name FROM users")
			require.NoError(t, err)
			defer qr.Close()

			cols, err := qr.Columns()
			require.NoError(t, err)
			assert.Equal(t, []string{"id", "name"}, cols)

			var names []string
			for qr.Next() {
				var id int
				var name string
				require.NoError(t, qr.Scan(&id, &name))
				names = append(names, name)
			}
			require.NoError(t, qr.Err())
			assert.Equal(t, []string{"alice", "bob"}, names)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_QueryError(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectQuery("SELECT boom").WillReturnError(sql.ErrConnDone)

			_, err := be.Query(context.Background(), "SELECT boom")
			assert.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_QueryRowScan(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectQuery("SELECT val").
				WillReturnRows(sqlmock.NewRows([]string{"val"}).AddRow("hello"))

			var val string
			require.NoError(t, be.QueryRow(context.Background(), "SELECT val FROM t").Scan(&val))
			assert.Equal(t, "hello", val)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_TableExists(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			// A matching row means the table exists. MySQL scans the name;
			// PostgreSQL scans an EXISTS boolean; SQL Server scans a count.
			var truthy *sqlmock.Rows
			switch tc.dialect {
			case DialectPostgreSQL:
				truthy = sqlmock.NewRows([]string{"exists"}).AddRow(true)
			case DialectSQLServer:
				truthy = sqlmock.NewRows([]string{"count"}).AddRow(1)
			default:
				truthy = sqlmock.NewRows([]string{"name"}).AddRow("users")
			}
			mock.ExpectQuery("(?i)information_schema|sys\\.objects|EXISTS").WillReturnRows(truthy)

			exists, err := be.TableExists(context.Background(), "users")
			require.NoError(t, err)
			assert.True(t, exists)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_ListTables(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectQuery("(?i)information_schema|sys\\.").
				WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("a").AddRow("b"))

			tables, err := be.ListTables(context.Background())
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b"}, tables)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_CreateAndDropTable(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec("DROP TABLE").WillReturnResult(sqlmock.NewResult(0, 0))

			def := TableDefinition{
				Name: "widgets",
				Columns: []ColumnDefinition{
					{Name: "id", Type: "INTEGER", Primary: true, AutoIncrement: true},
					{Name: "name", Type: "VARCHAR", Size: 100, Nullable: false},
				},
			}
			require.NoError(t, be.CreateTable(context.Background(), def))
			require.NoError(t, be.DropTable(context.Background(), "widgets"))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_MigrationAndLockTables(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectExec("(?is)(CREATE TABLE|IF NOT EXISTS).*version").
				WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec("(?is)(CREATE TABLE|IF NOT EXISTS).*lock_key").
				WillReturnResult(sqlmock.NewResult(0, 0))

			require.NoError(t, be.CreateMigrationTable(context.Background(), "schema_migrations"))
			require.NoError(t, be.CreateLockTable(context.Background(), "schema_migrations_lock"))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_GetVersion(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectQuery("(?i)version|@@VERSION").
				WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("1.2.3"))

			version, err := be.GetVersion(context.Background())
			require.NoError(t, err)
			assert.Equal(t, "1.2.3", version)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_TransactionCommit(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO t").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			tx, err := be.BeginTransaction(context.Background())
			require.NoError(t, err)

			_, err = tx.Execute(context.Background(), "INSERT INTO t (id) VALUES (1)")
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_TransactionRollback(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectBegin()
			mock.ExpectRollback()

			tx, err := be.BeginTransactionWithOptions(context.Background(), TransactionOptions{ReadOnly: true})
			require.NoError(t, err)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_TransactionSavepoints(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectBegin()
			if tc.dialect == DialectSQLServer {
				// SQL Server uses SAVE/ROLLBACK TRANSACTION and has no explicit
				// savepoint release (ReleaseSavepoint is a no-op, issuing no SQL).
				mock.ExpectExec("SAVE TRANSACTION").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ROLLBACK TRANSACTION").WillReturnResult(sqlmock.NewResult(0, 0))
			} else {
				mock.ExpectExec("SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("ROLLBACK TO SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("RELEASE SAVEPOINT").WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectCommit()

			tx, err := be.BeginTransaction(context.Background())
			require.NoError(t, err)
			require.NoError(t, tx.Savepoint("sp1"))
			require.NoError(t, tx.RollbackToSavepoint("sp1"))
			require.NoError(t, tx.ReleaseSavepoint("sp1"))
			require.NoError(t, tx.Commit())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_TransactionQueryRows(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT n FROM nums").
				WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(10).AddRow(20))
			mock.ExpectCommit()

			tx, err := be.BeginTransaction(context.Background())
			require.NoError(t, err)

			rows, err := tx.Query(context.Background(), "SELECT n FROM nums")
			require.NoError(t, err)
			var sum int
			for rows.Next() {
				var n int
				require.NoError(t, rows.Scan(&n))
				sum += n
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
			assert.Equal(t, 30, sum)

			// tx.QueryRow path.
			require.NoError(t, tx.Commit())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_GetDatabaseInfo(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)

			mock.ExpectQuery("(?i)version|@@VERSION").
				WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("9.9"))

			info, err := be.GetDatabaseInfo(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.dialect, info.Dialect)
			assert.Equal(t, "9.9", info.Version)
			assert.Equal(t, "app", info.Name)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSQLBackends_DisconnectClosesDB(t *testing.T) {
	for _, tc := range sqlBackendCases() {
		t.Run(tc.name, func(t *testing.T) {
			be, mock := newMockBackend(t, tc.make)
			mock.ExpectClose()

			assert.True(t, be.IsConnected())
			require.NoError(t, be.Disconnect(context.Background()))
			assert.False(t, be.IsConnected())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
