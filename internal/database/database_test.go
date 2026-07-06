/**
 * @file database_test.go
 * @description Tests for the database connection helper.
 * @author Admilson B. F. Cossa
 */

package database

import (
	"testing"
	"time"
)

func TestConnect_SQLiteInMemory(t *testing.T) {
	db, err := Connect(&Config{
		Driver:          "sqlite",
		DSN:             "file::memory:?cache=shared",
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("obtain sql.DB: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := sqlDB.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 5 {
		t.Errorf("MaxOpenConns = %d, want 5", got)
	}
}

func TestConnect_DriverAliases(t *testing.T) {
	for _, driver := range []string{"sqlite", "sqlite3"} {
		db, err := Connect(&Config{Driver: driver, DSN: "file::memory:?cache=shared"})
		if err != nil {
			t.Errorf("Connect(%q): %v", driver, err)
			continue
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

func TestConnect_NilConfig(t *testing.T) {
	if _, err := Connect(nil); err == nil {
		t.Error("Connect(nil) should error")
	}
}

func TestConnect_UnsupportedDriver(t *testing.T) {
	if _, err := Connect(&Config{Driver: "informix", DSN: "x"}); err == nil {
		t.Error("unsupported driver should error")
	}
}

func TestDialectorFor(t *testing.T) {
	for _, driver := range []string{"sqlite", "sqlite3", "mysql", "mariadb", "postgres", "postgresql", "pgx"} {
		if _, err := dialectorFor(driver, "dsn"); err != nil {
			t.Errorf("dialectorFor(%q) unexpected error: %v", driver, err)
		}
	}
	if _, err := dialectorFor("nope", "dsn"); err == nil {
		t.Error("dialectorFor(unsupported) should error")
	}
}
