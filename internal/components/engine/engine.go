/**
 * @file engine.go
 * @description Main migration engine implementation that integrates all migration components
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/engine/engine.go
 */

package engine

import (
	"context"
	"fmt"
	"sort"
	"time"

	adapters "github.com/go-gen-ecosystem/go-migrate/internal/components/adapter"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/executor"
	dbcore "github.com/go-gen-ecosystem/go-migrate/internal/database"

	loaderpkg "github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/lock"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"

	trackerpkg "github.com/go-gen-ecosystem/go-migrate/internal/components/tracker"
	validatorpkg "github.com/go-gen-ecosystem/go-migrate/internal/components/validator"
	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
	"gorm.io/gorm"
)

// MigrationEngine defines the main interface for migration operations.
//
// The lowercase fields hold the engine's live, wired components and are the
// source of truth used by all engine operations. The exported fields
// (Config, Backend, Tracker, ...) are optional inputs supplied by
// database-specific engine builders; when set, Initialize wires them into the
// live component fields. This keeps the builder API ergonomic (struct-literal
// construction) without exposing internal wiring details.
type MigrationEngine struct {
	config          types.Config
	loader          types.MigrationLoader
	executor        types.MigrationExecutor
	validator       types.MigrationValidator
	tracker         types.MigrationTracker
	backup          types.BackupManager
	health          types.HealthChecker
	rollbackManager types.RollbackManager
	initialized     bool
	db              *gorm.DB
	lockMgr         lock.Manager
	logger          logging.ComponentLogger

	// Builder-supplied component inputs (optional). Populated by the
	// database-specific engine builders and consumed by Initialize.
	Config      types.Config
	Backend     types.Backend
	LockManager types.LockManager
	Tracker     types.Tracker
	Executor    types.Executor
	Validator   types.Validator
	Resolver    types.Resolver
	Decorators  []types.Decorator
	Options     map[string]interface{}
	DryRun      bool
}

// adoptBuilderComponents copies any builder-supplied exported component inputs
// into the engine's live component fields. It is a no-op for fields that were
// not provided, preserving values wired by the factory constructors.
func (e *MigrationEngine) adoptBuilderComponents() {
	if e.config.Database.Driver == "" && e.Config.Database.Driver != "" {
		e.config = e.Config
	}
	if e.tracker == nil && e.Tracker != nil {
		e.tracker = e.Tracker
	}
	if e.executor == nil && e.Executor != nil {
		e.executor = e.Executor
	}
	if e.validator == nil && e.Validator != nil {
		e.validator = e.Validator
	}
}

// NewMigrationEngine creates a new migration engine instance using factory pattern
func NewMigrationEngine(config types.Config) (*MigrationEngine, error) {
	if config.Database.Driver == "" {
		return nil, fmt.Errorf("config cannot be empty - database driver is required")
	}

	engine := &MigrationEngine{
		config:      config,
		initialized: false,
		logger:      logging.GetComponentLogger("migration-engine"),
	}

	return engine, nil
}

// NewMigrationEngineWithFactory creates a new migration engine using the factory pattern
func NewMigrationEngineWithFactory(config types.Config, dryRun bool) (*MigrationEngine, error) {
	if config.Database.Driver == "" {
		return nil, fmt.Errorf("config cannot be empty - database driver is required")
	}

	// Initialize DB connection
	db, err := dbcore.Connect(context.Background(), config.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// Create engine with database-specific optimizations
	engine := &MigrationEngine{
		config: config,
		db:     db,
		logger: logging.GetComponentLogger("migration-engine"),
	}

	// Initialize components using adapters for compatibility
	fileLoader := loaderpkg.NewFileMigrationLoader()
	engine.loader = adapters.NewLoaderAdapter(fileLoader)

	dbTracker, err := trackerpkg.NewDatabaseMigrationTrackerWithDryRun(db, config.Migration.TableName, dryRun)
	if err != nil {
		return nil, fmt.Errorf("failed to create database migration tracker: %w", err)
	}
	engine.tracker = adapters.NewTrackerAdapter(dbTracker, db)

	engine.validator = validatorpkg.NewCompositeMigrationValidator(db, &config, engine.tracker)
	engine.executor = executorForDB(db, &config.Migration)
	engine.lockMgr = createDatabaseSpecificLockManagerFromDriver(config.Database.Driver, db, dryRun)
	engine.initialized = true

	return engine, nil
}

// Status returns the current migration status
func (e *MigrationEngine) Status(ctx context.Context) (*types.MigrationStatus, error) {
	return e.StatusWithDryRun(ctx, false)
}

// StatusWithDryRun returns the current migration status with optional dry run mode
func (e *MigrationEngine) StatusWithDryRun(ctx context.Context, dryRun bool) (*types.MigrationStatus, error) {
	if !e.initialized {
		if err := e.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize engine: %w", err)
		}
	}

	// Set dry-run mode on tracker if it supports it
	if trackerAdapter, ok := e.tracker.(interface{ SetDryRun(bool) }); ok {
		trackerAdapter.SetDryRun(dryRun)
	}

	// Ensure migration tracking tables exist before any tracker operations
	// Skip in dry run mode to prevent table creation
	if !dryRun {
		if err := e.tracker.EnsureSchemaTable(ctx); err != nil {
			return nil, fmt.Errorf("failed to ensure schema table: %w", err)
		}
	}

	// Gather status from tracker and loader
	// In dry-run mode, these should return empty results
	executed, err := e.tracker.GetExecutedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get executed migrations: %w", err)
	}

	failed, err := e.tracker.GetFailedMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get failed migrations: %w", err)
	}

	// Load migrations to compute pending
	migrations, err := e.loader.LoadMigrations(e.config.Migration.MigrationsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	pending := make([]types.MigrationRecord, 0)
	for _, m := range migrations {
		applied, err := e.tracker.IsMigrationApplied(ctx, m.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check migration applied: %w", err)
		}
		if !applied {
			pending = append(pending, types.MigrationRecord{
				ID:          m.ID,
				Version:     m.Version,
				Description: m.Description,
				Type:        string(m.Type),
				Script:      m.Script,
				Checksum:    m.Checksum,
				Success:     false,
				CanRollback: m.CanRollback,
				RollbackSQL: m.RollbackSQL,
			})
		}
	}

	// Determine lock status using lock manager
	lockID := "global-migrations"
	if e.config.Migration.LockTableName != "" {
		lockID = e.config.Migration.LockTableName
	}
	isLocked, err := e.lockMgr.IsLocked(ctx, lockID)
	if err != nil {
		return nil, fmt.Errorf("failed to get lock status: %w", err)
	}

	currentVersion, err := e.tracker.GetCurrentVersion(ctx)
	if err != nil {
		currentVersion = "unknown"
	}

	status := &types.MigrationStatus{
		TotalMigrations:   len(migrations),
		ExecutedCount:     len(executed),
		PendingCount:      len(pending),
		FailedCount:       len(failed),
		DatabaseVersion:   currentVersion,
		SchemaVersion:     currentVersion,
		IsLocked:          isLocked,
		PendingMigrations: pending,
		FailedMigrations:  failed,
	}
	// Set last migration if exists
	if len(executed) > 0 {
		last := executed[len(executed)-1]
		status.LastMigration = &last
	}
	return status, nil
}

// Migrate runs pending migrations
func (e *MigrationEngine) Migrate(ctx context.Context, target string, dryRun bool) error {
	if !e.initialized {
		if err := e.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
	}

	// Orchestrate using a simple pipeline
	pctx := &PipelineContext{Target: target, DryRun: dryRun}

	// Create a unified pipeline with all necessary steps
	pipeline := NewPipeline(
		// Set dry run mode on tracker adapter
		StepFunc{name: "set_dry_run_mode", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			if trackerAdapter, ok := eng.tracker.(*adapters.TrackerAdapter); ok {
				trackerAdapter.SetDryRun(p.DryRun)
			}
			return nil
		}},
		// Schema table creation step (skip in dry run mode)
		StepFunc{name: "ensure_schema_table", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			// Skip schema table creation in dry run mode
			if p.DryRun || eng.config.Migration.EnableDryRun {
				return nil
			}
			// Ensure migration tracking tables exist before any tracker operations
			if err := eng.tracker.EnsureSchemaTable(ctx); err != nil {
				return fmt.Errorf("failed to ensure schema table: %w", err)
			}
			return nil
		}},
		StepFunc{name: "acquire_lock", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			// Skip lock acquisition in dry run mode
			if p.DryRun || eng.config.Migration.EnableDryRun {
				return nil
			}
			lockID := "global-migrations"
			if eng.config.Migration.LockTableName != "" {
				lockID = eng.config.Migration.LockTableName
			}
			lockObj, err := eng.lockMgr.AcquireLock(ctx, lockID, eng.config.Migration.LockTimeout)
			if err != nil {
				return fmt.Errorf("failed to acquire migration lock: %w", err)
			}
			p.Lock = lockObj
			return nil
		}},
		StepFunc{name: "pre_backup", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			// Create backup before running migrations if enabled
			if eng.backup != nil && (eng.config.Migration.EnableBackup || eng.config.Features.AutoBackup) {
				name := fmt.Sprintf("pre-migrate-%d", time.Now().Unix())
				if err := eng.backup.CreateBackup(ctx, name); err != nil {
					p.HadError = true
					if eng.config.Migration.StopOnError {
						return fmt.Errorf("failed to create backup: %w", err)
					}
				} else {
					p.BackupName = name
				}
			}
			return nil
		}},
		StepFunc{name: "pre_health_check", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			if eng.health != nil && (eng.config.Features.HealthChecks) {
				if err := eng.health.CheckHealth(ctx); err != nil {
					p.HadError = true
					if eng.config.Migration.StopOnError {
						return fmt.Errorf("pre-migration health check failed: %w", err)
					}
				} else if status, err := eng.health.GetHealthStatus(ctx); err == nil {
					p.HealthPre = status
				}
			}
			return nil
		}},
		StepFunc{name: "load_migrations", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			migrations, err := eng.loader.LoadMigrations(eng.config.Migration.MigrationsPath)
			if err != nil {
				return fmt.Errorf("failed to load migrations: %w", err)
			}
			// Convert to pointers
			p.Migrations = make([]*types.Migration, len(migrations))
			for i := range migrations {
				p.Migrations[i] = &migrations[i]
			}
			return nil
		}},
		StepFunc{name: "filter_pending", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			// In dry run mode, treat all migrations as pending since tracking tables don't exist
			if p.DryRun || eng.config.Migration.EnableDryRun {
				p.Pending = p.Migrations
				// Apply target filtering if specified
				if p.Target != "" {
					for i, m := range p.Migrations {
						if m.Version == p.Target || m.ID == p.Target {
							p.Pending = p.Migrations[:i+1]
							break
						}
					}
				}
				return nil
			}

			pending, err := filterPendingMigrations(ctx, p.Migrations, eng.tracker, p.Target)
			if err != nil {
				return fmt.Errorf("failed to filter pending migrations: %w", err)
			}
			p.Pending = pending
			return nil
		}},
		StepFunc{name: "resolve_dependencies", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			if len(p.Pending) == 0 {
				return nil
			}

			// Use dependency resolver to get proper execution order
			resolver := eng.getDependencyResolver()
			ordered, err := resolver.ResolveDependencies(ctx, p.Pending)
			if err != nil {
				return fmt.Errorf("failed to resolve dependencies: %w", err)
			}
			p.Pending = ordered
			return nil
		}},
		StepFunc{name: "validate_migrations", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			if eng.validator == nil {
				return nil
			}

			for _, migration := range p.Pending {
				if err := eng.validator.ValidateMigration(migration); err != nil {
					p.HadError = true
					if eng.config.Migration.StopOnError {
						return fmt.Errorf("validation failed for migration %s: %w", migration.ID, err)
					}
					// Log validation error but continue
					p.ValidationErrors = append(p.ValidationErrors, fmt.Sprintf("Migration %s: %v", migration.ID, err))
				}
			}
			return nil
		}},
		StepFunc{name: "execute_migrations", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			// Dry run applies nothing: no SQL is executed and no records are
			// written, so the target database is left untouched.
			if p.DryRun || eng.config.Migration.EnableDryRun {
				return nil
			}
			for _, migration := range p.Pending {
				start := time.Now()
				if err := eng.executor.Execute(ctx, migration); err != nil {
					// Persist the failure for auditability and dirty-state
					// detection (RecordMigration upserts by ID, so a later
					// successful retry overwrites this row).
					if recErr := eng.tracker.RecordMigrationFailure(ctx, migrationToRecord(migration), err.Error()); recErr != nil {
						eng.logger.WithContext(ctx).Warn("failed to record migration failure",
							"migration", migration.ID, "error", recErr)
					}
					p.HadError = true
					p.ExecutionErrors = append(p.ExecutionErrors, fmt.Sprintf("Migration %s: %v", migration.ID, err))
					if eng.config.Migration.StopOnError {
						return fmt.Errorf("failed to execute migration %s: %w", migration.ID, err)
					}
					continue
				}
				// Record success so the migration is filtered out on the next run.
				if err := eng.tracker.RecordMigrationSuccess(ctx, migrationToRecord(migration), time.Since(start).Milliseconds()); err != nil {
					return fmt.Errorf("migration %s executed but failed to record: %w", migration.ID, err)
				}
				p.ExecutedMigrations = append(p.ExecutedMigrations, migration)
			}
			return nil
		}},
		StepFunc{name: "post_health_check", fn: func(ctx context.Context, eng *MigrationEngine, p *PipelineContext) error {
			if eng.health != nil && eng.config.Features.HealthChecks {
				if err := eng.health.CheckHealth(ctx); err != nil {
					p.HadError = true
					if eng.config.Migration.StopOnError {
						return fmt.Errorf("post-migration health check failed: %w", err)
					}
				} else if status, err := eng.health.GetHealthStatus(ctx); err == nil {
					p.HealthPost = status
				}
			}
			return nil
		}},
	)

	// Execute the unified pipeline
	err := pipeline.Run(ctx, e, pctx)
	// Ensure lock release regardless of pipeline outcome
	if pctx.Lock != nil {
		_ = e.lockMgr.ReleaseLock(ctx, pctx.Lock)
	}
	return err
}

// Rollback rolls back migrations
func (e *MigrationEngine) Rollback(ctx context.Context, steps int) error {
	return e.RollbackWithDryRun(ctx, steps, false)
}

// RollbackWithDryRun rolls back migrations with optional dry run mode
func (e *MigrationEngine) RollbackWithDryRun(ctx context.Context, steps int, dryRun bool) error {
	e.logger.WithContext(ctx).Debug("Starting rollback operation", "steps", steps, "dryRun", dryRun)

	if !e.initialized {
		if err := e.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
	}

	if steps <= 0 {
		e.logger.WithContext(ctx).Debug("Rollback returning early - no steps to rollback", "steps", steps)
		return nil
	}

	// Ensure migration tracking tables exist before any tracker operations
	// Skip in dry run mode to prevent table creation
	if !dryRun {
		if err := e.tracker.EnsureSchemaTable(ctx); err != nil {
			return fmt.Errorf("failed to ensure schema table: %w", err)
		}
	}

	applied, err := e.tracker.GetAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}
	e.logger.WithContext(ctx).Debug("Retrieved applied migrations", "count", len(applied))
	for i, m := range applied {
		e.logger.WithContext(ctx).Debug("Applied migration found", "index", i, "id", m.ID, "version", m.Version)
	}

	// Determine migrations to rollback
	maxDepth := e.config.Migration.MaxRollbackDepth
	minSteps := min(steps, maxDepth)
	e.logger.WithContext(ctx).Debug("Planning rollback", "requestedSteps", steps, "maxRollbackDepth", maxDepth, "actualSteps", minSteps)
	toRollback := pickLast(applied, minSteps)
	e.logger.WithContext(ctx).Info("Rollback plan created", "appliedMigrations", len(applied), "migrationsToRollback", len(toRollback))

	for i, m := range toRollback {
		e.logger.WithContext(ctx).Debug("Rolling back migration", "step", i+1, "total", len(toRollback), "id", m.ID, "version", m.Version)

		// Execute the rollback SQL (or simulate in dry run mode)
		if dryRun {
			e.logger.WithContext(ctx).Info("DRY RUN - Would rollback migration", "id", m.ID)
		} else {
			if err := e.executor.Rollback(ctx, m); err != nil {
				e.logger.WithContext(ctx).Error("Failed to execute rollback SQL", err)
				return fmt.Errorf("failed to rollback %s: %w", m.ID, err)
			}
			e.logger.WithContext(ctx).Debug("Successfully executed rollback SQL", "id", m.ID)

			// Remove the migration record from the tracker
			if err := e.tracker.RemoveMigration(ctx, m.ID); err != nil {
				e.logger.WithContext(ctx).Error("Failed to remove migration from tracker", err)
				return fmt.Errorf("failed to remove migration %s from tracker: %w", m.ID, err)
			}
			e.logger.WithContext(ctx).Debug("Successfully removed migration from tracker", "id", m.ID)
		}
	}
	return nil
}

// Validate validates all migrations
func (e *MigrationEngine) Validate(ctx context.Context) error {
	if !e.initialized {
		if err := e.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
	}

	migrations, err := e.loader.LoadMigrations(e.config.Migration.MigrationsPath)
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Run composite validation
	if e.validator == nil {
		return nil
	}
	if err := e.validator.ValidateMigrations(migrations); err != nil {
		return fmt.Errorf("migrations validation failed: %w", err)
	}
	return nil
}

// Info returns migration information
func (e *MigrationEngine) Info(ctx context.Context) (*types.MigrationInfo, error) {
	if !e.initialized {
		if err := e.Initialize(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize engine: %w", err)
		}
	}

	return &types.MigrationInfo{
		Version:        "1.0.0",
		DatabaseType:   e.config.Database.Driver,
		MigrationsPath: e.config.Migration.MigrationsPath,
		TableName:      e.config.Migration.TableName,
		LockTableName:  e.config.Migration.LockTableName,
		Environment:    e.config.Environment,
		Features:       []string{},
		LastCheck:      time.Now(),
	}, nil
}

// Close closes the migration engine and releases resources
func (e *MigrationEngine) Close() error {
	// Implementation would close connections and cleanup resources
	e.initialized = false
	if e.db != nil {
		if sqlDB, err := e.db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		e.db = nil
	}
	return nil
}

// SetRollbackManager sets the rollback manager for the engine
func (e *MigrationEngine) SetRollbackManager(rollbackManager types.RollbackManager) {
	e.rollbackManager = rollbackManager
}

// GetRollbackManager returns the rollback manager for the engine
func (e *MigrationEngine) GetRollbackManager() types.RollbackManager {
	return e.rollbackManager
}

// Initialize initializes the migration engine
func (e *MigrationEngine) Initialize(ctx context.Context) error {
	if e.initialized {
		return nil
	}

	// Adopt any components supplied via builder struct-literal construction.
	e.adoptBuilderComponents()

	// Initialize DB connection
	db, err := dbcore.Connect(ctx, e.config.Database)
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}
	e.db = db

	// Debug: Check tables before tracker creation
	var tablesBefore []string
	e.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tablesBefore)
	e.logger.WithContext(ctx).Debug("Tables before tracker creation", "tables", tablesBefore)

	// Initialize loader via adapter (avoid signature mismatch)
	fileLoader := loaderpkg.NewFileMigrationLoader()
	e.loader = adapters.NewLoaderAdapter(fileLoader)

	// Initialize tracker and lock manager only if not already set by factory
	if e.tracker == nil {
		dbTracker, err := trackerpkg.NewDatabaseMigrationTracker(db, e.config.Migration.TableName)
		if err != nil {
			return fmt.Errorf("failed to create database migration tracker: %w", err)
		}
		e.tracker = adapters.NewTrackerAdapter(dbTracker, db)
	}

	// Debug: Check tables after tracker creation
	var tablesAfter []string
	e.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tablesAfter)
	e.logger.WithContext(ctx).Debug("Tables after tracker creation", "tables", tablesAfter)

	// Initialize validator (composite)
	e.validator = validatorpkg.NewCompositeMigrationValidator(db, &e.config, e.tracker)

	// Initialize executor
	e.executor = executorForDB(db, &e.config.Migration)

	// Initialize lock manager only if not already set by factory
	if e.lockMgr == nil {
		e.lockMgr = e.createDatabaseSpecificLockManager(db, false) // Default to non-dry-run for backward compatibility
	}

	e.initialized = true
	return nil
}

// createDatabaseSpecificLockManager creates a lock manager optimized for the specific database type
func (e *MigrationEngine) createDatabaseSpecificLockManager(db *gorm.DB, dryRun bool) lock.Manager {
	// Use in-memory lock manager for dry run mode to avoid database table access
	if dryRun {
		return lock.NewInMemoryLockManager()
	}
	// Use the generic DatabaseLockManager for all database types
	// This provides consistent behavior across all supported databases
	return lock.NewDatabaseLockManagerWithDryRun(db, e.config.Migration.LockTableName, dryRun)
}

// createDatabaseSpecificLockManagerFromDriver creates a lock manager based on driver string
func createDatabaseSpecificLockManagerFromDriver(driver string, db *gorm.DB, dryRun bool) lock.Manager {
	// Use in-memory lock manager for dry run mode to avoid database table access
	if dryRun {
		return lock.NewInMemoryLockManager()
	}
	// Use the generic DatabaseLockManager for all database types
	// This provides consistent behavior across all supported databases
	return lock.NewDatabaseLockManagerWithDryRun(db, lock.DefaultMigrationLockTableName, dryRun)
}

// Helper: filter pending migrations and optionally target up to specific version/id
func filterPendingMigrations(ctx context.Context, migrations []*types.Migration, tracker types.MigrationTracker, target string) ([]*types.Migration, error) {
	pending := make([]*types.Migration, 0, len(migrations))

	for i := range migrations {
		m := migrations[i]
		applied, err := tracker.IsMigrationApplied(ctx, m.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to check if migration %s is applied: %w", m.ID, err)
		}
		if !applied {
			pending = append(pending, m)
		}

		// Stop at target if specified
		if target != "" && (m.Version == target || m.ID == target) {
			break
		}
	}

	return pending, nil
}

func (e *MigrationEngine) getDependencyResolver() types.DependencyResolver {
	// Return a simple dependency resolver that sorts by version
	return &simpleDependencyResolver{}
}

type simpleDependencyResolver struct{}

func (r *simpleDependencyResolver) ResolveDependencies(ctx context.Context, migrations []*types.Migration) ([]*types.Migration, error) {
	sortByVersion(migrations)
	return migrations, nil
}

func planExecutionOrder(migrations []*types.Migration, cfg types.Config) ([]*types.Migration, error) {
	// Sort migrations by version for execution order
	sortByVersion(migrations)

	// Parallel execution mode ("parallel") would group migrations differently,
	// but for now all modes keep the sequential version order established above.

	// Check for circular dependencies
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	for _, migration := range migrations {
		if !visited[migration.ID] {
			if hasCycle := checkCyclicDependencies(migration, migrations, visited, recursionStack); hasCycle {
				return nil, fmt.Errorf("circular dependency detected involving migration %s", migration.ID)
			}
		}
	}

	return migrations, nil
}

func checkCyclicDependencies(migration *types.Migration, allMigrations []*types.Migration, visited, recursionStack map[string]bool) bool {
	visited[migration.ID] = true
	recursionStack[migration.ID] = true

	// Check dependencies
	for _, depID := range migration.Dependencies {
		if !visited[depID] {
			// Find the dependency migration
			var depMigration *types.Migration
			for _, m := range allMigrations {
				if m.ID == depID {
					depMigration = m
					break
				}
			}
			if depMigration != nil && checkCyclicDependencies(depMigration, allMigrations, visited, recursionStack) {
				return true
			}
		} else if recursionStack[depID] {
			return true // Cycle detected
		}
	}

	recursionStack[migration.ID] = false
	return false
}

func sortByVersion(ms []*types.Migration) {
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].Version < ms[j].Version
	})
}

func pickLast(applied []*types.Migration, n int) []*types.Migration {
	if len(applied) == 0 {
		return []*types.Migration{}
	}

	start := len(applied) - n
	if start < 0 {
		start = 0
	}

	// Return in reverse order for rollback (most recent first)
	result := make([]*types.Migration, 0, n)
	for i := len(applied) - 1; i >= start; i-- {
		result = append(result, applied[i])
	}

	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// migrationToRecord projects a loaded migration into the tracker's record shape,
// carrying only the fields the tracker persists when marking a migration applied
// or failed. The tracker sets state and timestamps itself.
func migrationToRecord(m *types.Migration) types.MigrationRecord {
	return types.MigrationRecord{
		ID:          m.ID,
		Version:     m.Version,
		Description: m.Description,
		Type:        string(m.Type),
		Script:      m.Script,
		Checksum:    m.Checksum,
		Author:      m.Author,
		Success:     true,
		CanRollback: m.CanRollback,
		RollbackSQL: m.RollbackSQL,
	}
}

func executorForDB(db *gorm.DB, cfg *types.MigrationConfig) types.MigrationExecutor {
	return executor.NewDatabaseMigrationExecutor(db, cfg)
}

func wrapInTransaction(exec types.MigrationExecutor, db *gorm.DB) types.MigrationExecutor {
	return exec // Placeholder - would wrap in transaction
}

func wrapInDryRun(exec types.MigrationExecutor) types.MigrationExecutor {
	return exec // Placeholder - would wrap in dry run
}

type monitoringExecutor struct {
	base    types.MigrationExecutor
	monitor types.Monitor
}

func (m *monitoringExecutor) Execute(ctx context.Context, mig *types.Migration) error {
	if m.monitor != nil {
		// Monitoring hooks are best-effort: a failed metric write must not
		// abort or alter the migration execution itself.
		_ = m.monitor.RecordMigrationStart(ctx, mig.ID)
		start := time.Now()
		defer func() {
			duration := time.Since(start)
			success := true // This would need to be determined based on actual execution result
			_ = m.monitor.RecordMigrationEnd(ctx, mig.ID, success, duration.Nanoseconds())
		}()
	}
	return m.base.Execute(ctx, mig)
}

func (m *monitoringExecutor) ExecuteDryRun(ctx context.Context, mig *types.Migration) (*types.ExecutionPlan, error) {
	return m.base.ExecuteDryRun(ctx, mig)
}

func (m *monitoringExecutor) Rollback(ctx context.Context, mig *types.Migration) error {
	return m.base.Rollback(ctx, mig)
}

func (m *monitoringExecutor) ValidateRollback(ctx context.Context, mig *types.Migration) error {
	return m.base.ValidateRollback(ctx, mig)
}

func (m *monitoringExecutor) ValidateMigration(ctx context.Context, mig *types.Migration) error {
	return m.base.ValidateMigration(ctx, mig)
}

func (m *monitoringExecutor) GetExecutionPlan(ms []*types.Migration) ([]string, error) {
	return m.base.GetExecutionPlan(ms)
}

func wrapWithMonitoring(exec types.MigrationExecutor, mon types.Monitor) types.MigrationExecutor {
	return &monitoringExecutor{base: exec, monitor: mon}
}

// PipelineContext holds ephemeral state shared across steps
type PipelineContext struct {
	Target     string
	DryRun     bool
	Lock       *lock.MigrationLock
	Migrations []*types.Migration
	Pending    []*types.Migration
	Planned    []*types.Migration

	// Execution results
	ExecutedMigrations []*types.Migration
	ExecutionPlans     []*types.ExecutionPlan

	// Error tracking
	HadError         bool
	ValidationErrors []string
	ExecutionErrors  []string

	// Optional integration state
	BackupName string
	HealthPre  map[string]interface{}
	HealthPost map[string]interface{}
}

// Step represents a unit of work in the pipeline
type Step interface {
	Name() string
	Run(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error
}

// StepFunc is a functional adapter for Step
type StepFunc struct {
	name string
	fn   func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error
}

// NewStepFunc creates a new StepFunc with the given name and function
func NewStepFunc(name string, fn func(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error) StepFunc {
	return StepFunc{
		name: name,
		fn:   fn,
	}
}

// Name returns the step's identifier.
func (s StepFunc) Name() string { return s.name }

// Run executes the step's wrapped function against the engine and pipeline context.
func (s StepFunc) Run(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
	return s.fn(ctx, eng, pctx)
}

// Pipeline composes steps and runs them sequentially
type Pipeline struct {
	steps []Step
}

// NewPipeline creates a new pipeline
func NewPipeline(steps ...Step) *Pipeline {
	p := &Pipeline{steps: make([]Step, 0)}
	return p.Add(steps...)
}

// Add appends steps to the pipeline
func (p *Pipeline) Add(steps ...Step) *Pipeline {
	p.steps = append(p.steps, steps...)
	return p
}

// Run executes all steps sequentially, failing fast on error
func (p *Pipeline) Run(ctx context.Context, eng *MigrationEngine, pctx *PipelineContext) error {
	for _, step := range p.steps {
		if err := step.Run(ctx, eng, pctx); err != nil {
			return err
		}
	}
	return nil
}
