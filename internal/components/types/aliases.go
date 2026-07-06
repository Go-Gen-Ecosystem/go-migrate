/**
 * @file aliases.go
 * @description Short type aliases for engine component interfaces. Engine
 *              builders across database backends refer to the core interfaces
 *              via these concise names; the aliases keep a single source of
 *              truth while preserving readable builder call sites.
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path internal/components/types/aliases.go
 */

package types

// Component interface aliases used by engine builders.
//
// These alias the canonical interface definitions declared in
// engine_interfaces.go and types.go so that database-specific builders can
// reference them with short, consistent names (e.g. types.Backend rather than
// types.EngineBackend).
type (
	// Backend aliases EngineBackend, the bridge implementor for DB-specific ops.
	Backend = EngineBackend

	// Tracker aliases MigrationTracker, the migration state tracking contract.
	Tracker = MigrationTracker

	// Executor aliases MigrationExecutor, the migration execution contract.
	Executor = MigrationExecutor

	// Validator aliases MigrationValidator, the migration validation contract.
	Validator = MigrationValidator

	// Resolver aliases DependencyResolver, the dependency resolution contract.
	Resolver = DependencyResolver

	// MigrationEngine aliases MigrationEngineInterface, the engine contract
	// returned by builders' Build() methods.
	MigrationEngine = MigrationEngineInterface
)

// MigrationDecorator defines the Decorator contract for wrapping migration
// engines with cross-cutting concerns (logging, metrics, caching, retries).
// A decorator receives a MigrationEngine and returns a wrapped MigrationEngine.
type MigrationDecorator interface {
	Decorate(engine MigrationEngine) MigrationEngine
	Name() string
}

// Decorator aliases MigrationDecorator for concise builder call sites.
type Decorator = MigrationDecorator
