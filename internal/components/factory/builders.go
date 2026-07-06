/**
 * @file builders.go
 * @description Factory builder functions for different database engine types
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/factory/builders.go
 */

package factory

import (
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/mongodb"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/mysql"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/engines/oracle"
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

// NewOracleEngineBuilder creates a new Oracle engine builder
func NewOracleEngineBuilder() types.EngineBuilder {
	return oracle.NewOracleEngineBuilder()
}

// NewDB2EngineBuilder creates a new DB2 engine builder
func NewDB2EngineBuilder() types.EngineBuilder {
	// DB2 implementation not yet available, return placeholder for now
	return &PlaceholderEngineBuilder{dbType: "db2"}
}

// NoSQL Database Engine Builders

// NewMongoDBEngineBuilder creates a new MongoDB engine builder
func NewMongoDBEngineBuilder() types.EngineBuilder {
	return mongodb.NewMongoDBEngineBuilder()
}

// NewCassandraEngineBuilder creates a new Cassandra engine builder
func NewCassandraEngineBuilder() types.EngineBuilder {
	// Cassandra implementation not yet available, return placeholder for now
	return &PlaceholderEngineBuilder{dbType: "cassandra"}
}

// NewRedisEngineBuilder creates a new Redis engine builder
func NewRedisEngineBuilder() types.EngineBuilder {
	// Redis implementation not yet available, return placeholder for now
	return &PlaceholderEngineBuilder{dbType: "redis"}
}

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

// NewOracleComponentsFactory creates a new Oracle components factory
func NewOracleComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual Oracle components factory
	return &PlaceholderComponentsFactory{dialect: "oracle"}
}

// NewDB2ComponentsFactory creates a new DB2 components factory
func NewDB2ComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual DB2 components factory
	return &PlaceholderComponentsFactory{dialect: "db2"}
}

// NewMongoDBComponentsFactory creates a new MongoDB components factory
func NewMongoDBComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual MongoDB components factory
	return &PlaceholderComponentsFactory{dialect: "mongodb"}
}

// NewCassandraComponentsFactory creates a new Cassandra components factory
func NewCassandraComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual Cassandra components factory
	return &PlaceholderComponentsFactory{dialect: "cassandra"}
}

// NewRedisComponentsFactory creates a new Redis components factory
func NewRedisComponentsFactory() types.ComponentsFactory {
	// TODO: Implement actual Redis components factory
	return &PlaceholderComponentsFactory{dialect: "redis"}
}
