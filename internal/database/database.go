/**
 * @file database.go
 * @description Minimal database connection helper for the migration engine.
 * Reconstructs the Config/Connect API the engine was written against (it
 * previously lived in the gogen monorepo's pkg/core/database package) on top
 * of GORM, which the tracker/validator layers already require.
 * @author Admilson B. F. Cossa
 */

package database

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Config describes a database connection and its pool settings.
type Config struct {
	Driver          string
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	LogLevel        int
}

// Connect opens a GORM connection for the configured driver and applies the
// pool settings. Supported drivers: sqlite, mysql, postgres (and their common
// aliases).
func Connect(cfg *Config) (*gorm.DB, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config is nil")
	}

	dialector, err := dialectorFor(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.LogLevel(cfg.LogLevel + 1)),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s connection: %w", cfg.Driver, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("obtain underlying sql.DB: %w", err)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	return db, nil
}

// dialectorFor maps a driver name to a GORM dialector.
func dialectorFor(driver, dsn string) (gorm.Dialector, error) {
	switch driver {
	case "sqlite", "sqlite3":
		return sqlite.Open(dsn), nil
	case "mysql", "mariadb":
		return mysql.Open(dsn), nil
	case "postgres", "postgresql", "pgx":
		return postgres.Open(dsn), nil
	default:
		return nil, fmt.Errorf("unsupported database driver: %q", driver)
	}
}
