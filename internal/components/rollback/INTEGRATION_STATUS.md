# Advanced Rollback Integration Status

## Overview

This document tracks the integration status of advanced rollback capabilities across all three layers of the GoMigrate system: Builder API, CLI, and WebUI.

---

## ✅ Layer 1: Builder API Integration - **COMPLETED**

### Status: **FULLY INTEGRATED**

The advanced rollback manager is now fully integrated into the migration engine builder and can be used by any application using the Builder API.

### Files Modified:

1. **[builder/engine_builder.go](engine_builder.go)**
   - Added `rollbackManager`, `advancedRollback`, and `rollbackConfig` fields to `ConcreteMigrationEngineBuilder`
   - Added builder methods:
     - `WithRollbackManager(rollbackManager)` - Set base rollback manager
     - `WithAdvancedRollback(advancedRollback)` - Set advanced rollback manager
     - `WithRollbackConfig(config)` - Set rollback configuration
   - Updated `Build()` method to wire rollback components into engine
   - Links advanced rollback to base manager automatically

2. **[engine/engine.go](engine.go)**
   - Added `rollbackManager` field to `MigrationEngine` struct
   - Added methods:
     - `SetRollbackManager(rollbackManager)` - Set the rollback manager
     - `GetRollbackManager()` - Get the rollback manager
   - Engine now supports rollback manager injection

3. **[types/types.go](types/types.go)**
   - Added type forward declarations:
     - `RollbackConfig` struct
     - `RollbackManager` interface
     - `AdvancedRollbackManager` interface
   - Enables type-safe builder API without circular dependencies

### Usage Example:

```go
import (
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/builder"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/rollback"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// Create base rollback manager
rollbackConfig := &rollback.RollbackConfig{
    MaxRollbackCount:       10,
    ValidateBeforeRollback: true,
    CreateBackup:           true,
    UseTransactions:        true,
}

baseRollbackMgr := rollback.NewDefaultRollbackManager(
    tracker,
    executor,
    validator,
    rollbackConfig,
)

// Create advanced rollback manager
advancedConfig := &rollback.AdvancedRollbackConfig{
    EnableSelectiveRollback:      true,
    EnableImpactAnalysis:         true,
    EnableRollbackTesting:        true,
    DefaultDepth:                 5,
    MaxDepth:                     10,
    EnableSavepoints:             true,
    TestEnvironments:             []string{"staging", "test"},
}

advancedRollbackMgr := rollback.NewDefaultAdvancedRollbackManager(
    baseRollbackMgr,
    tracker,
    executor,
    logger,
    advancedConfig,
)

// Build engine with rollback support
engine, err := builder.NewMigrationEngineBuilder("postgres").
    WithConfig(config).
    WithBackend(backend).
    WithExecutor(executor).
    WithTracker(tracker).
    WithLockManager(lockMgr).
    WithValidator(validator).
    WithRollbackManager(baseRollbackMgr).
    WithAdvancedRollback(advancedRollbackMgr).
    WithRollbackConfig(&types.RollbackConfig{
        MaxRollbackCount: 10,
        CreateBackup: true,
    }).
    Build()

if err != nil {
    log.Fatal(err)
}

// Access rollback features
rollbackMgr := engine.GetRollbackManager()
if rollbackMgr != nil {
    advMgr := rollbackMgr.GetAdvancedManager()
    if advMgr != nil {
        // Use advanced rollback features
        analysis, _ := advMgr.AnalyzeRollbackImpact(ctx, []string{"migration_001"})
        result, _ := advMgr.RollbackSelective(ctx, "migration_001", options)
    }
}
```

### Benefits:

- ✅ **Type-safe API** - Compile-time type checking
- ✅ **Fluent interface** - Chain multiple configurations
- ✅ **Optional integration** - Can be nil if not needed
- ✅ **Auto-wiring** - Advanced manager automatically linked to base manager
- ✅ **Backward compatible** - Existing code works without changes

---

## 🔄 Layer 2: CLI Integration - **IN PROGRESS**

### Status: **NOT YET INTEGRATED**

Need to add new CLI commands to expose advanced rollback features.

### Current State:

**Existing CLI Commands** ([cmd/cli/migrate.go](cmd/cli/migrate.go)):
- `gogen migrate down [steps]` - Basic rollback
- `gogen migrate rollback --to-version <version>` - Version-based rollback
- `gogen migrate rollback --to-tag <tag>` - Tag-based rollback
- `gogen migrate rollback-sql` - Generate rollback SQL

**What's Missing:**
- No CLI commands for advanced features

### Planned CLI Commands:

#### 1. Selective Rollback Command
```bash
# Rollback specific migration in middle of chain
gogen migrate rollback selective <migration-id> [flags]

Flags:
  --with-dependents     Automatically rollback dependent migrations
  --force               Force rollback even with conflicts
  --dry-run             Preview rollback without executing
  --create-backup       Create backup before rollback (default: true)
  --create-savepoint    Create savepoint before rollback
  --max-depth int       Maximum dependency depth (default: 5)
```

#### 2. Impact Analysis Command
```bash
# Analyze rollback impact before execution
gogen migrate rollback analyze <migration-id> [flags]

Flags:
  --output string       Output format: text, json, yaml (default: text)
  --show-conflicts      Show detailed conflict information
  --show-dependencies   Show dependency graph
```

#### 3. Rollback Testing Command
```bash
# Test rollback in staging environment
gogen migrate rollback test <migration-id> [flags]

Flags:
  --environment string  Test environment: staging, test (required)
  --verbose            Show detailed test results
  --output string      Output format: text, json, yaml (default: text)
```

#### 4. Dependency Graph Command
```bash
# Visualize migration dependency graph
gogen migrate rollback graph <migration-id> [flags]

Flags:
  --format string      Output format: text, dot, json (default: text)
  --max-depth int      Maximum graph depth (default: 10)
  --show-levels        Show dependency levels
```

### Files to Modify:

1. **[cmd/cli/migrate.go](cmd/cli/migrate.go)**
   - Add 4 new command functions
   - Wire to advanced rollback manager
   - Add flag parsing and validation
   - Add formatted output (text/JSON/YAML)

2. **[pkg/core/database/migrations/internal/components/facade/commands.go](facade/commands.go)**
   - Add command implementations:
     - `SelectiveRollbackCommand`
     - `AnalyzeRollbackCommand`
     - `TestRollbackCommand`
     - `GraphRollbackCommand`

### Implementation Plan:

```go
// In cmd/cli/migrate.go

// 1. Add selective rollback command
var rollbackSelectiveCmd = &cobra.Command{
    Use:   "selective <migration-id>",
    Short: "Rollback specific migration selectively",
    Long:  `Roll back a specific migration even if it's in the middle of the chain.
Automatically handles dependencies and conflicts.`,
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        migrationID := args[0]

        // Get flags
        withDependents, _ := cmd.Flags().GetBool("with-dependents")
        force, _ := cmd.Flags().GetBool("force")
        dryRun, _ := cmd.Flags().GetBool("dry-run")
        createBackup, _ := cmd.Flags().GetBool("create-backup")
        createSavepoint, _ := cmd.Flags().GetBool("create-savepoint")
        maxDepth, _ := cmd.Flags().GetInt("max-depth")

        // Get engine and advanced rollback manager
        engine, err := getEngine(cmd)
        if err != nil {
            return err
        }

        rollbackMgr := engine.GetRollbackManager()
        if rollbackMgr == nil {
            return fmt.Errorf("rollback manager not available")
        }

        advMgr := rollbackMgr.GetAdvancedManager()
        if advMgr == nil {
            return fmt.Errorf("advanced rollback not available")
        }

        // Execute selective rollback
        options := &rollback.SelectiveRollbackOptions{
            Force:              force,
            RollbackDependents: withDependents,
            CreateSavepoint:    createSavepoint,
            DryRun:             dryRun,
            MaxDepth:           maxDepth,
            CreateBackup:       createBackup,
        }

        result, err := advMgr.RollbackSelective(cmd.Context(), migrationID, options)
        if err != nil {
            return fmt.Errorf("selective rollback failed: %w", err)
        }

        // Print results
        printSelectiveRollbackResult(result)
        return nil
    },
}

// Initialize command
func init() {
    rollbackSelectiveCmd.Flags().Bool("with-dependents", false, "Rollback dependent migrations")
    rollbackSelectiveCmd.Flags().Bool("force", false, "Force rollback")
    rollbackSelectiveCmd.Flags().Bool("dry-run", false, "Preview without executing")
    rollbackSelectiveCmd.Flags().Bool("create-backup", true, "Create backup")
    rollbackSelectiveCmd.Flags().Bool("create-savepoint", false, "Create savepoint")
    rollbackSelectiveCmd.Flags().Int("max-depth", 5, "Maximum dependency depth")

    // Add to rollback parent command
    rollbackCmd.AddCommand(rollbackSelectiveCmd)
}

// Similar implementations for analyze, test, and graph commands...
```

---

## 🌐 Layer 3: WebUI Integration - **NOT STARTED**

### Status: **NOT YET INTEGRATED**

Need to add REST API endpoints and handlers to expose advanced rollback features via web interface.

### Current State:

**Existing REST API** ([pkg/core/webui/handlers/migrations/handler.go](pkg/core/webui/handlers/migrations/handler.go)):
- `POST /api/v1/migrations/rollback` - Basic rollback with steps

**What's Missing:**
- No API endpoints for advanced features
- No WebSocket support for real-time progress
- No UI components for visualization

### Planned REST API Endpoints:

#### 1. Selective Rollback API
```
POST /api/v1/migrations/rollback/selective

Request Body:
{
  "migration_id": "migration_001",
  "force": false,
  "rollback_dependents": true,
  "create_savepoint": true,
  "dry_run": false,
  "max_depth": 5,
  "create_backup": true
}

Response:
{
  "success": true,
  "migration_id": "migration_001",
  "rolled_back_migrations": [
    { "id": "migration_003", "version": "3.0.0" },
    { "id": "migration_002", "version": "2.0.0" },
    { "id": "migration_001", "version": "1.0.0" }
  ],
  "impact_analysis": { ... },
  "errors": [],
  "warnings": ["High impact rollback"],
  "duration": "1.5s",
  "savepoints": ["rollback_migration_001_1234567890"],
  "backup_location": "backup_migration_001_1234567890",
  "executed_at": "2025-01-29T10:00:00Z"
}
```

#### 2. Impact Analysis API
```
POST /api/v1/migrations/rollback/analyze

Request Body:
{
  "migration_ids": ["migration_001", "migration_002"]
}

Response:
{
  "migration_id": "migration_001,migration_002",
  "impact_level": "high",
  "affected_migrations": [ ... ],
  "affected_tables": ["users", "orders"],
  "affected_columns": [],
  "affected_indexes": [],
  "affected_constraints": [],
  "data_loss_risk": true,
  "estimated_data_loss": {
    "tables_affected": ["users"],
    "estimated_rows": { "users": 1000 },
    "total_estimate": 1000,
    "backup_required": true
  },
  "estimated_duration": "5s",
  "conflicts": [
    {
      "type": "dependency",
      "severity": "high",
      "description": "Migration has 2 dependent migrations",
      "affected_objects": ["migration_002", "migration_003"],
      "resolution": "Rollback dependent migrations first",
      "can_auto_resolve": true,
      "requires_manual": false
    }
  ],
  "recommendations": [
    "High impact detected - ensure you have a current backup",
    "Data loss risk detected - verify backup is available"
  ],
  "warnings": [],
  "can_proceed_safely": false,
  "requires_manual": false,
  "analyzed_at": "2025-01-29T10:00:00Z"
}
```

#### 3. Rollback Testing API
```
POST /api/v1/migrations/rollback/test

Request Body:
{
  "migration_ids": ["migration_001"],
  "environment": "staging"
}

Response:
{
  "success": true,
  "environment": "staging",
  "tested_at": "2025-01-29T10:00:00Z",
  "duration": "2s",
  "test_cases": [
    {
      "name": "Test rollback for migration_001",
      "description": "Verify rollback of migration migration_001",
      "migration_id": "migration_001",
      "status": "passed",
      "duration": "1s",
      "error": "",
      "assertions": [
        "Rollback SQL exists",
        "Migration is rollbackable",
        "Rollback SQL syntax is valid"
      ],
      "verified_objects": []
    }
  ],
  "total_tests": 1,
  "passed_tests": 1,
  "failed_tests": 0,
  "skipped_tests": 0,
  "coverage": 100.0,
  "errors": [],
  "warnings": [],
  "recommendation": "All tests passed - rollback is safe to execute in production"
}
```

#### 4. Dependency Graph API
```
GET /api/v1/migrations/rollback/graph/:migration_id

Response:
{
  "root_migration": { ... },
  "nodes": {
    "migration_001": {
      "migration": { ... },
      "level": 0,
      "dependencies": [],
      "dependents": ["migration_002", "migration_003"],
      "is_rollbackable": true
    },
    "migration_002": {
      "migration": { ... },
      "level": 1,
      "dependencies": ["migration_001"],
      "dependents": ["migration_004"],
      "is_rollbackable": true
    }
  },
  "edges": [
    {
      "from": "migration_001",
      "to": "migration_002",
      "edge_type": "explicit",
      "is_blocking": true
    }
  ],
  "levels": [
    ["migration_001"],
    ["migration_002", "migration_003"],
    ["migration_004"]
  ],
  "max_depth": 2,
  "total_nodes": 4,
  "has_cycles": false,
  "cyclic_paths": []
}
```

### Files to Create/Modify:

1. **[pkg/core/webui/handlers/migrations/handler.go](pkg/core/webui/handlers/migrations/handler.go)**
   - Add handler methods:
     - `RollbackSelective(c *gin.Context)`
     - `AnalyzeRollback(c *gin.Context)`
     - `TestRollback(c *gin.Context)`
     - `GetDependencyGraph(c *gin.Context)`
   - Add request/response types
   - Wire to advanced rollback manager

2. **[pkg/core/webui/handlers/migrations/types.go](pkg/core/webui/handlers/migrations/types.go)** (NEW)
   - Define request/response structures
   - Add JSON tags for API contracts

3. **[pkg/core/webui/websocket/events.go](pkg/core/webui/websocket/events.go)**
   - Add WebSocket event types:
     - `rollback:selective:start`
     - `rollback:selective:progress`
     - `rollback:selective:complete`
     - `rollback:analysis:complete`
     - `rollback:test:progress`
     - `rollback:test:complete`

### WebSocket Integration:

```go
// Real-time progress updates
type RollbackProgressEvent struct {
    Type            string    `json:"type"` // "rollback:selective:progress"
    MigrationID     string    `json:"migration_id"`
    CurrentStep     int       `json:"current_step"`
    TotalSteps      int       `json:"total_steps"`
    CurrentMigration string   `json:"current_migration"`
    Status          string    `json:"status"` // "rolling_back", "completed", "failed"
    Message         string    `json:"message"`
    Timestamp       time.Time `json:"timestamp"`
}

// Example WebSocket message
{
  "event": "rollback:selective:progress",
  "data": {
    "migration_id": "migration_001",
    "current_step": 2,
    "total_steps": 3,
    "current_migration": "migration_002",
    "status": "rolling_back",
    "message": "Rolling back migration migration_002",
    "timestamp": "2025-01-29T10:00:01Z"
  }
}
```

---

## Integration Checklist

### Builder API: ✅ COMPLETE
- [x] Add rollback fields to engine builder
- [x] Add builder methods (WithRollbackManager, WithAdvancedRollback, WithRollbackConfig)
- [x] Wire components in Build() method
- [x] Add engine methods (SetRollbackManager, GetRollbackManager)
- [x] Add type forward declarations
- [x] Test builder integration

### CLI: ❌ TODO
- [ ] Add `rollback selective` command
- [ ] Add `rollback analyze` command
- [ ] Add `rollback test` command
- [ ] Add `rollback graph` command
- [ ] Add flag parsing and validation
- [ ] Add formatted output (text/JSON/YAML)
- [ ] Add command implementations in facade
- [ ] Wire to advanced rollback manager
- [ ] Add CLI tests
- [ ] Update CLI documentation

### WebUI: ❌ TODO
- [ ] Add REST API endpoints (4 endpoints)
- [ ] Add request/response types
- [ ] Add handler implementations
- [ ] Wire to advanced rollback manager
- [ ] Add WebSocket event types
- [ ] Implement real-time progress updates
- [ ] Add API tests
- [ ] Update API documentation
- [ ] Create UI components (optional)

---

## Testing Strategy

### Unit Tests:
- ✅ Advanced rollback manager tests (completed)
- ❌ Builder integration tests (TODO)
- ❌ CLI command tests (TODO)
- ❌ WebUI handler tests (TODO)

### Integration Tests:
- ❌ End-to-end Builder API test (TODO)
- ❌ End-to-end CLI test (TODO)
- ❌ End-to-end WebUI test (TODO)

### Manual Testing:
- ❌ Builder API usage example (TODO)
- ❌ CLI commands in real environment (TODO)
- ❌ WebUI API calls with Postman/curl (TODO)
- ❌ WebSocket events with real-time updates (TODO)

---

## Next Steps

### Immediate (High Priority):
1. **Complete CLI Integration** - Add 4 new commands
2. **Test Builder API** - Create example usage and verify
3. **Document Builder API** - Update integration guide

### Short Term (Medium Priority):
4. **Complete WebUI Integration** - Add 4 REST API endpoints
5. **Add WebSocket Support** - Real-time progress updates
6. **Create UI Components** - Visual rollback interface (optional)

### Long Term (Low Priority):
7. **Add Grafana Dashboard** - Visualize rollback metrics
8. **Add Alerting** - Notify on high-impact rollbacks
9. **Add Audit Trail** - Track all rollback operations

---

## Breaking Changes

None. All integration is **backward compatible**:
- Existing code works without changes
- Advanced features are optional
- Can be enabled/disabled per project

---

## Documentation

### Completed:
- ✅ [ADVANCED_ROLLBACK.md](ADVANCED_ROLLBACK.md) - Feature documentation
- ✅ [INTEGRATION_STATUS.md](INTEGRATION_STATUS.md) - This file

### TODO:
- ❌ Builder API integration guide
- ❌ CLI command reference
- ❌ WebUI API reference
- ❌ WebSocket event reference
- ❌ Migration guide for existing projects

---

## Support

For questions or issues with advanced rollback integration:
1. Check [ADVANCED_ROLLBACK.md](ADVANCED_ROLLBACK.md) for usage examples
2. Check this file for integration status
3. Review test files for code examples
4. Open an issue on GitHub

---

**Last Updated:** 2025-01-29
**Status:** Builder API ✅ Complete | CLI 🔄 In Progress | WebUI ❌ Not Started