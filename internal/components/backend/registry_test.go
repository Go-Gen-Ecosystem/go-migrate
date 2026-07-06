/**
 * @file registry_test.go
 * @description Tests for the backend registry, dialect metadata, backend factory
 *              construction and the backend builder. Covers all four SQL dialects:
 *              the SQLite backend is exercised end-to-end elsewhere; MySQL,
 *              PostgreSQL and SQL Server backends are constructed and their config
 *              driven DSN/adaptation behavior validated without a live server.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/backend/registry_test.go
 */

package backend

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestNewBackendRegistry_DialectMetadata(t *testing.T) {
	reg := NewBackendRegistry()

	// The registry pre-populates metadata for every known dialect.
	cases := []struct {
		dialect DatabaseDialect
		name    string
		port    int
	}{
		{DialectPostgreSQL, "PostgreSQL", 5432},
		{DialectMySQL, "MySQL", 3306},
		{DialectSQLite, "SQLite", 0},
		{DialectSQLServer, "SQL Server", 1433},
		{DialectOracle, "Oracle Database", 1521},
		{DialectDB2, "IBM DB2", 50000},
		{DialectCassandra, "Apache Cassandra", 9042},
		{DialectMongoDB, "MongoDB", 27017},
		{DialectRedis, "Redis", 6379},
	}

	for _, tc := range cases {
		t.Run(string(tc.dialect), func(t *testing.T) {
			info, err := reg.GetDialectInfo(tc.dialect)
			require.NoError(t, err)
			assert.Equal(t, tc.name, info.Name)
			assert.Equal(t, tc.port, info.DefaultPort)
			assert.NotEmpty(t, info.Features)
			assert.NotEmpty(t, info.URLSchemes)
		})
	}
}

func TestBackendRegistry_GetDialectInfoUnknown(t *testing.T) {
	reg := NewBackendRegistry()
	_, err := reg.GetDialectInfo(DatabaseDialect("nonexistent"))
	assert.Error(t, err)
}

func TestBackendRegistry_RegisterGetUnregister(t *testing.T) {
	reg := NewBackendRegistry()
	factory := NewSQLiteBackendFactory()

	// Nothing registered initially.
	assert.False(t, reg.IsSupported(DialectSQLite))
	assert.Empty(t, reg.List())

	require.NoError(t, reg.Register(DialectSQLite, factory))
	assert.True(t, reg.IsSupported(DialectSQLite))
	assert.Equal(t, []DatabaseDialect{DialectSQLite}, reg.List())

	got, err := reg.Get(DialectSQLite)
	require.NoError(t, err)
	assert.Same(t, factory, got)

	require.NoError(t, reg.Unregister(DialectSQLite))
	assert.False(t, reg.IsSupported(DialectSQLite))

	_, err = reg.Get(DialectSQLite)
	assert.Error(t, err)
}

func TestBackendRegistry_RegisterValidation(t *testing.T) {
	reg := NewBackendRegistry()

	assert.Error(t, reg.Register("", NewSQLiteBackendFactory()), "empty dialect must fail")
	assert.Error(t, reg.Register(DialectSQLite, nil), "nil factory must fail")
	assert.Error(t, reg.Unregister(DialectSQLite), "unregistering an absent dialect must fail")
}

func TestGlobalRegistry_AllSQLDialectsRegistered(t *testing.T) {
	// The package init() functions register all four SQL backends globally.
	for _, d := range []DatabaseDialect{DialectSQLite, DialectMySQL, DialectPostgreSQL, DialectSQLServer} {
		assert.True(t, IsDialectSupported(d), "dialect %s should be registered globally", d)
	}

	supported := ListSupportedDialects()
	assert.Subset(t, supported,
		[]DatabaseDialect{DialectSQLite, DialectMySQL, DialectPostgreSQL, DialectSQLServer})
}

func TestGlobalRegistry_CreateBackendPerDialect(t *testing.T) {
	cases := []struct {
		dialect DatabaseDialect
		want    DatabaseDialect
	}{
		{DialectSQLite, DialectSQLite},
		{DialectMySQL, DialectMySQL},
		{DialectPostgreSQL, DialectPostgreSQL},
		{DialectSQLServer, DialectSQLServer},
	}

	for _, tc := range cases {
		t.Run(string(tc.dialect), func(t *testing.T) {
			be, err := CreateBackend(tc.dialect, types.DatabaseConfig{Database: "app"})
			require.NoError(t, err)
			require.NotNil(t, be)
			assert.Equal(t, tc.want, be.GetDialect())
			assert.False(t, be.IsConnected(), "a freshly created backend must not be connected")
		})
	}
}

func TestGlobalRegistry_CreateBackendUnsupported(t *testing.T) {
	_, err := CreateBackend(DatabaseDialect("nosuchdb"), types.DatabaseConfig{})
	assert.Error(t, err)

	_, err = GetBackend(DatabaseDialect("nosuchdb"))
	assert.Error(t, err)
}

func TestGetDialectInfo_Global(t *testing.T) {
	info, err := GetDialectInfo(DialectSQLite)
	require.NoError(t, err)
	assert.Equal(t, "SQLite", info.Name)
	assert.Contains(t, info.FileExtensions, ".db")
}

func TestBackendFactories_PerDialect(t *testing.T) {
	factories := map[DatabaseDialect]BackendFactory{
		DialectSQLite:     NewSQLiteBackendFactory(),
		DialectMySQL:      NewMySQLBackendFactory(),
		DialectPostgreSQL: NewPostgreSQLBackendFactory(),
		DialectSQLServer:  NewSQLServerBackendFactory(),
	}

	for dialect, factory := range factories {
		t.Run(string(dialect), func(t *testing.T) {
			assert.Equal(t, []DatabaseDialect{dialect}, factory.SupportedDialects())

			info := factory.GetDialectInfo(dialect)
			assert.NotEmpty(t, info.Name)

			be, err := factory.CreateBackend(dialect, types.DatabaseConfig{Database: "app"})
			require.NoError(t, err)
			assert.Equal(t, dialect, be.GetDialect())

			// A factory rejects a dialect it does not own.
			_, err = factory.CreateBackend(DatabaseDialect("wrong"), types.DatabaseConfig{})
			assert.Error(t, err)
		})
	}
}

func TestDefaultBackendFactory_Creator(t *testing.T) {
	called := false
	factory := NewBackendFactory(DialectSQLite, func(cfg types.DatabaseConfig) (DatabaseBackend, error) {
		called = true
		return NewSQLiteBackend(cfg)
	})

	be, err := factory.CreateBackend(DialectSQLite, types.DatabaseConfig{})
	require.NoError(t, err)
	require.NotNil(t, be)
	assert.True(t, called, "creator func must be invoked")

	// Mismatched dialect short-circuits before calling the creator.
	_, err = factory.CreateBackend(DialectMySQL, types.DatabaseConfig{})
	assert.Error(t, err)
}

func TestBackendBuilder_ConfigAndBuild(t *testing.T) {
	builder := NewBackendBuilder(DialectSQLite).
		WithHost("localhost").
		WithPort(5432).
		WithDatabase("app").
		WithUsername("user").
		WithPassword("secret").
		WithSSL(true).
		WithTimeout(30).
		WithMaxConnections(10).
		WithOptions(map[string]interface{}{"opt1": "val1", "num": 5})

	assert.Equal(t, DialectSQLite, builder.GetDialect())

	cfg := builder.GetConfig()
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "app", cfg.Database)
	assert.Equal(t, "user", cfg.Username)
	assert.Equal(t, "secret", cfg.Password)
	assert.Equal(t, "require", cfg.SSLMode)
	assert.Equal(t, 30*time.Second, cfg.ConnectionTimeout)
	assert.Equal(t, 10, cfg.MaxConnections)
	assert.Equal(t, "val1", cfg.Options["opt1"])
	assert.Equal(t, "5", cfg.Options["num"], "non-string options are stringified")

	be, err := builder.Build()
	require.NoError(t, err)
	assert.Equal(t, DialectSQLite, be.GetDialect())
}

func TestBackendBuilder_WithSSLDisable(t *testing.T) {
	cfg := NewBackendBuilder(DialectPostgreSQL).WithSSL(false).GetConfig()
	assert.Equal(t, "disable", cfg.SSLMode)
}

// TestMySQLBackend_ConstructAndDSN validates the MySQL backend can be built from
// config and that its dialect + SQL adaptation are correct without a live server.
func TestMySQLBackend_ConstructAndDSN(t *testing.T) {
	be, err := NewMySQLBackend(types.DatabaseConfig{
		Host:              "db.example.com",
		Port:              3306,
		Database:          "app",
		Username:          "root",
		Password:          "pass",
		ConnectionTimeout: 5 * time.Second,
		SSLMode:           "require",
	})
	require.NoError(t, err)
	assert.Equal(t, DialectMySQL, be.GetDialect())
	assert.False(t, be.IsConnected())

	// Operating on an unconnected backend surfaces a clear error.
	_, err = be.Execute(context.Background(), "SELECT 1")
	assert.Error(t, err)

	// AdaptSQL applies MySQL-specific rewrites.
	assert.Equal(t, "SELECT `id` FROM `t`", be.AdaptSQL(`SELECT "id" FROM "t"`))
	assert.Equal(t, "id AUTO_INCREMENT", be.AdaptSQL("id SERIAL"))

	// ValidateSQL rejects dangerous operations.
	assert.Error(t, be.ValidateSQL("DROP DATABASE app"))
	assert.Error(t, be.ValidateSQL("TRUNCATE TABLE users"))
	assert.NoError(t, be.ValidateSQL("SELECT * FROM users"))
}

// TestPostgreSQLBackend_ConstructAndAdapt validates the PostgreSQL backend build
// path, dialect and SQL adaptation without a live server.
func TestPostgreSQLBackend_ConstructAndAdapt(t *testing.T) {
	be, err := NewPostgreSQLBackend(types.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "app",
		Username: "postgres",
		SSLMode:  "disable",
	})
	require.NoError(t, err)
	assert.Equal(t, DialectPostgreSQL, be.GetDialect())
	assert.False(t, be.IsConnected())

	_, err = be.Query(context.Background(), "SELECT 1")
	assert.Error(t, err)

	assert.Equal(t, `SELECT "id"`, be.AdaptSQL("SELECT `id`"))
	assert.Equal(t, "id SERIAL", be.AdaptSQL("id AUTO_INCREMENT"))
	assert.Equal(t, "id SERIAL", be.AdaptSQL("id AUTOINCREMENT"))

	assert.Error(t, be.ValidateSQL("DROP DATABASE app"))
	assert.NoError(t, be.ValidateSQL("SELECT 1"))
}

// TestSQLServerBackend_ConstructAndAdapt validates the SQL Server backend build
// path, dialect and SQL adaptation without a live server.
func TestSQLServerBackend_ConstructAndAdapt(t *testing.T) {
	be, err := NewSQLServerBackend(types.DatabaseConfig{
		Host:     "localhost",
		Port:     1433,
		Database: "app",
		Username: "sa",
		Password: "Str0ng!",
	})
	require.NoError(t, err)
	assert.Equal(t, DialectSQLServer, be.GetDialect())
	assert.False(t, be.IsConnected())

	_, err = be.Execute(context.Background(), "SELECT 1")
	assert.Error(t, err)

	assert.Equal(t, "id IDENTITY(1,1)", be.AdaptSQL("id AUTO_INCREMENT"))
	assert.Equal(t, "SELECT GETDATE()", be.AdaptSQL("SELECT NOW()"))
	assert.Equal(t, "SELECT TOP 10 * FROM t", be.AdaptSQL("SELECT LIMIT 10 * FROM t"))

	assert.Error(t, be.ValidateSQL("DROP DATABASE app"))
	assert.Error(t, be.ValidateSQL("SHUTDOWN"))
	assert.NoError(t, be.ValidateSQL("SELECT 1"))
}
