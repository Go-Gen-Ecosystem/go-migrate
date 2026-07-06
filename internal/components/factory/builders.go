/**
 * @file builders.go
 * @description Factory builder functions for different database engine types
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/factory/builders.go
 */

package factory

import (
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/mysql"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/postgres"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/sqlite"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/sqlserver"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// SQL Database Engine Builders

// NewPostgreSQLEngineBuilder creates a new PostgreSQL engine builder
func NewPostgreSQLEngineBuilder() types.EngineBuilder {
	return postgres.NewPostgresEngineBuilder()
}

// NewMySQLEngineBuilder creates a new MySQL engine builder
func NewMySQLEngineBuilder() types.EngineBuilder {
	return mysql.NewMySQLEngineBuilder()
}

// NewSQLiteEngineBuilder creates a new SQLite engine builder
func NewSQLiteEngineBuilder() types.EngineBuilder {
	return sqlite.NewSQLiteEngineBuilder()
}

// NewSQLServerEngineBuilder creates a new SQL Server engine builder
func NewSQLServerEngineBuilder() types.EngineBuilder {
	return sqlserver.NewSQLServerEngineBuilder()
}

// NoSQL Database Engine Builders

// Components Factory Builders

// NewPostgreSQLComponentsFactory creates a new PostgreSQL components factory
func NewPostgreSQLComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual PostgreSQL components factory
	return &PlaceholderComponentsFactory{dialect: "postgresql"}
}

// NewMySQLComponentsFactory creates a new MySQL components factory
func NewMySQLComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual MySQL components factory
	return &PlaceholderComponentsFactory{dialect: "mysql"}
}

// NewSQLiteComponentsFactory creates a new SQLite components factory
func NewSQLiteComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual SQLite components factory
	return &PlaceholderComponentsFactory{dialect: "sqlite"}
}

// NewSQLServerComponentsFactory creates a new SQL Server components factory
func NewSQLServerComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual SQL Server components factory
	return &PlaceholderComponentsFactory{dialect: "sqlserver"}
}
