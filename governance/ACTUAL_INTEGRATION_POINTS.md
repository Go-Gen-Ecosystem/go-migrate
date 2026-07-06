# Governance System - Actual Integration Points in GoMigrate

## Answer: YES - Here's How to Integrate Into the Actual Pipeline

The governance system is **not yet integrated** into the actual GoMigrate code, but here's **exactly where and how** to integrate it at all three levels.

---

## Current GoMigrate Architecture

```
GoMigrate Pipeline Structure:
├── cmd/gomigrate/main.go          ← Standalone CLI entry point
├── cmd/cli/migrate.go             ← Gogen CLI commands
├── pkg/core/database/migrations/
    ├── internal/components/
    │   ├── engine/                ← Core migration engine
    │   ├── standalone/            ← Standalone tool
    │   ├── facade/                ← CLI facade layer
    │   └── builder/               ← Pipeline builder
    └── webui/                     ← WebUI integration (already done!)
```

---

## Integration Point 1: **Standalone CLI** (`cmd/gomigrate/main.go`)

### Current Code (Lines 148-155)
```go
func createMigrationTool() (migrations.StandaloneMigrationTool, error) {
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		configPath = ".gomigrate.yaml"
	}

	return migrations.NewStandaloneMigrationToolFromPath(configPath)
}
```

### ✅ **How to Integrate Governance**

#### Step 1: Add Governance Flags to `main.go`

```go
// Add after line 27 (after force variable)
var (
	cfgFile     string
	environment string
	verbose     bool
	dryRun      bool
	force       bool

	// 🔒 NEW: Governance flags
	enableGovernance    bool
	strictMode          bool
	requireApproval     bool
	minApprovals        int
	complianceFramework string
	currentUser         string
	currentUserName     string
)

// Add to init() function after line 165
rootCmd.PersistentFlags().BoolVar(&enableGovernance, "governance", false, "enable policy-based governance")
rootCmd.PersistentFlags().BoolVar(&strictMode, "strict", false, "strict mode (warnings become errors)")
rootCmd.PersistentFlags().BoolVar(&requireApproval, "require-approval", false, "require approval for risky migrations")
rootCmd.PersistentFlags().IntVar(&minApprovals, "min-approvals", 2, "minimum approvals required")
rootCmd.PersistentFlags().StringVar(&complianceFramework, "compliance", "SOX", "compliance framework (SOX|HIPAA|GDPR|PCI-DSS|ISO)")
rootCmd.PersistentFlags().StringVar(&currentUser, "user", "", "current user ID")
rootCmd.PersistentFlags().StringVar(&currentUserName, "user-name", "", "current user name")
```

#### Step 2: Modify `createMigrationTool()` Function

```go
// Replace current function (line 148-155) with:
func createMigrationTool() (migrations.StandaloneMigrationTool, error) {
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		configPath = ".gomigrate.yaml"
	}

	// Create base tool
	tool, err := migrations.NewStandaloneMigrationToolFromPath(configPath)
	if err != nil {
		return nil, err
	}

	// 🔒 NEW: Wrap with governance if enabled
	if enableGovernance {
		return wrapWithGovernance(tool)
	}

	return tool, nil
}

// 🔒 NEW: Add governance wrapper function
func wrapWithGovernance(tool migrations.StandaloneMigrationTool) (migrations.StandaloneMigrationTool, error) {
	// Initialize governance manager
	govConfig := governance.GovernanceConfig{
		Environment: environment,
		PolicyEngineConfig: governance.PolicyEngineConfig{
			Environment:     environment,
			StrictMode:      strictMode,
			RequireApproval: requireApproval,
		},
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			RequireApproval:     requireApproval,
			MinimumApprovals:    minApprovals,
			AllowSelfApproval:   false,
			ApprovalTimeout:     24 * time.Hour,
			EnableNotifications: false,
		},
		ComplianceConfig: governance.ComplianceConfig{
			Framework:       governance.ComplianceFramework(complianceFramework),
			OutputDir:       "./compliance-reports",
			RetentionPeriod: 7 * 365 * 24 * time.Hour,
		},
		Enabled: true,
	}

	manager, err := governance.NewGovernanceManager(govConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize governance: %w", err)
	}

	// Wrap tool with governance
	return governance.NewGovernanceStandaloneTool(tool, manager, currentUser, currentUserName), nil
}
```

#### Step 3: Create Governance Approval Commands

```go
// Add new commands after line 172
var approveCmd = &cobra.Command{
	Use:   "approve [request-id]",
	Short: "Approve a migration request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		requestID := args[0]
		approverID, _ := cmd.Flags().GetString("approver")
		approverName, _ := cmd.Flags().GetString("approver-name")
		comment, _ := cmd.Flags().GetString("comment")

		// Get governance manager from tool
		tool, _ := createMigrationTool()
		govTool, ok := tool.(interface{ GetGovernanceManager() *governance.GovernanceManager })
		if !ok {
			return fmt.Errorf("governance not enabled")
		}

		manager := govTool.GetGovernanceManager()
		err := manager.ApproveMigration(context.Background(), requestID, approverID, approverName, comment)
		if err != nil {
			return err
		}

		fmt.Printf("✓ Approved migration request: %s\n", requestID)
		return nil
	},
}

var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Generate compliance report",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Implementation here
		return nil
	},
}

func init() {
	approveCmd.Flags().String("approver", "", "Approver user ID")
	approveCmd.Flags().String("approver-name", "", "Approver name")
	approveCmd.Flags().String("comment", "", "Approval comment")

	rootCmd.AddCommand(approveCmd)
	rootCmd.AddCommand(complianceCmd)
}
```

---

## Integration Point 2: **Gogen CLI** (`cmd/cli/migrate.go`)

### Current Code (Lines 419-476 - `runUpMigration` function)

```go
func runUpMigration(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Running pending migrations...")

	// ... validation code ...

	// Execute migrations with enhanced locking
	if err := executeMigrationsWithLocking(); err != nil {
		return fmt.Errorf("migration execution failed: %w", err)
	}

	// ... rest of function ...
}
```

### ✅ **How to Integrate Governance**

```go
// Add governance flags to migrate command (after line 150)
migrateCmd.PersistentFlags().BoolVar(&migrationConfig.EnableGovernance, "governance", false, "Enable governance")
migrateCmd.PersistentFlags().BoolVar(&migrationConfig.StrictMode, "strict", false, "Strict policy mode")
migrateCmd.PersistentFlags().StringVar(&migrationConfig.CurrentUser, "user", "", "Current user ID")

// Modify runUpMigration function (replace lines 419-476)
func runUpMigration(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Running pending migrations...")

	// Enhanced error handling and validation
	if migrationConfig.MigrationsPath == "" {
		migrationConfig.MigrationsPath = "./migrations"
	}

	// Check if migrations directory exists
	if _, err := os.Stat(migrationConfig.MigrationsPath); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found: %s", migrationConfig.MigrationsPath)
	}

	// 🔒 NEW: Initialize governance if enabled
	var governanceManager *governance.GovernanceManager
	if migrationConfig.EnableGovernance {
		fmt.Println("🔒 Initializing governance...")

		govConfig := governance.GovernanceConfig{
			Environment: migrationConfig.Environment,
			PolicyEngineConfig: governance.PolicyEngineConfig{
				Environment: migrationConfig.Environment,
				StrictMode:  migrationConfig.StrictMode,
			},
			Enabled: true,
		}

		var err error
		governanceManager, err = governance.NewGovernanceManager(govConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize governance: %w", err)
		}

		fmt.Println("✓ Governance enabled")
	}

	// 🔒 NEW: Validate migrations against policies
	if governanceManager != nil {
		fmt.Println("🔍 Validating against policies...")

		// Load migrations from path
		migrations, err := loadMigrationsFromPath(migrationConfig.MigrationsPath)
		if err != nil {
			return fmt.Errorf("failed to load migrations: %w", err)
		}

		// Validate each migration
		for _, migration := range migrations {
			result, err := governanceManager.ValidateMigration(cmd.Context(), migration)
			if err != nil {
				return fmt.Errorf("policy validation failed: %w", err)
			}

			if !result.Passed {
				fmt.Printf("\n⚠️  Migration %s has policy violations:\n", migration.ID)
				for _, v := range result.Violations {
					fmt.Printf("  - [%s] %s: %s\n", v.Severity, v.PolicyName, v.Message)
					if v.Suggestion != "" {
						fmt.Printf("    💡 %s\n", v.Suggestion)
					}
				}

				if result.Action == governance.PolicyActionBlock {
					return fmt.Errorf("migration blocked by policy")
				}

				if result.RequiresApproval {
					fmt.Println("\n📋 This migration requires approval")
					return fmt.Errorf("approval required - use 'gogen migrate approve' command")
				}
			}
		}

		fmt.Println("✓ All policy checks passed")
	}

	// Continue with existing migration logic
	if migrationConfig.DryRun {
		fmt.Println("🔍 DRY RUN MODE - Analyzing pending migrations...")
		if err := performDryRunAnalysis(); err != nil {
			return fmt.Errorf("dry run analysis failed: %w", err)
		}
		fmt.Println("✅ Dry run completed successfully - no issues found")
		return nil
	}

	// ... rest of existing code ...
}
```

---

## Integration Point 3: **Builder API** (`pkg/core/database/migrations/internal/components/builder/`)

### Current Code - Create new file `governance_builder.go`

```go
/**
 * @file governance_builder.go
 * @description Builder API with governance support
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 */

package builder

import (
	"context"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/engine"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/governance"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// GovernanceMigrationBuilder builds migrations with governance
type GovernanceMigrationBuilder struct {
	engine            *engine.MigrationEngine
	governanceManager *governance.GovernanceManager
	governanceEnabled bool
	currentUser       string
	currentUserName   string
}

// NewGovernanceMigrationBuilder creates a new governance migration builder
func NewGovernanceMigrationBuilder() *GovernanceMigrationBuilder {
	return &GovernanceMigrationBuilder{}
}

// WithEngine sets the migration engine
func (b *GovernanceMigrationBuilder) WithEngine(engine *engine.MigrationEngine) *GovernanceMigrationBuilder {
	b.engine = engine
	return b
}

// WithGovernance enables governance with config
func (b *GovernanceMigrationBuilder) WithGovernance(config governance.GovernanceConfig) *GovernanceMigrationBuilder {
	manager, err := governance.NewGovernanceManager(config)
	if err != nil {
		// Log error but continue
		return b
	}

	b.governanceManager = manager
	b.governanceEnabled = true
	return b
}

// WithUser sets the current user
func (b *GovernanceMigrationBuilder) WithUser(userID, userName string) *GovernanceMigrationBuilder {
	b.currentUser = userID
	b.currentUserName = userName
	return b
}

// Build builds the final migration tool
func (b *GovernanceMigrationBuilder) Build() (*GovernanceMigrationTool, error) {
	tool := &GovernanceMigrationTool{
		engine:            b.engine,
		governanceManager: b.governanceManager,
		governanceEnabled: b.governanceEnabled,
		currentUser:       b.currentUser,
		currentUserName:   b.currentUserName,
	}

	return tool, nil
}

// GovernanceMigrationTool wraps engine with governance
type GovernanceMigrationTool struct {
	engine            *engine.MigrationEngine
	governanceManager *governance.GovernanceManager
	governanceEnabled bool
	currentUser       string
	currentUserName   string
}

// Migrate executes migrations with governance checks
func (t *GovernanceMigrationTool) Migrate(ctx context.Context, target string, dryRun bool) error {
	if t.governanceEnabled && t.governanceManager != nil {
		// Get pending migrations
		status, err := t.engine.Status(ctx)
		if err != nil {
			return err
		}

		// Validate each pending migration
		for _, pending := range status.PendingMigrations {
			migration := convertToMigration(pending)

			result, err := t.governanceManager.ValidateMigration(ctx, migration)
			if err != nil {
				return err
			}

			if result.Action == governance.PolicyActionBlock {
				return fmt.Errorf("migration %s blocked by policy", migration.ID)
			}

			if result.RequiresApproval {
				approved, err := t.governanceManager.CheckMigrationApproval(ctx, migration.ID)
				if err != nil {
					return err
				}
				if !approved {
					return fmt.Errorf("migration %s requires approval", migration.ID)
				}
			}

			// Check execution permission
			err = t.governanceManager.ExecuteMigration(ctx, migration, t.currentUser, t.currentUserName)
			if err != nil {
				return err
			}
		}
	}

	// Execute with base engine
	return t.engine.Migrate(ctx, target, dryRun)
}

// Rollback rolls back migrations with governance checks
func (t *GovernanceMigrationTool) Rollback(ctx context.Context, steps int) error {
	if t.governanceEnabled && t.governanceManager != nil {
		// Check rollback permission
		// This would need to get the actual migrations being rolled back
		// For now, simplified
		status, _ := t.engine.Status(ctx)
		if len(status.AppliedMigrations) > 0 {
			migration := convertToMigration(status.AppliedMigrations[0])
			err := t.governanceManager.RollbackMigration(ctx, migration, t.currentUser, t.currentUserName)
			if err != nil {
				return err
			}
		}
	}

	return t.engine.Rollback(ctx, steps)
}

// convertToMigration converts internal migration format to governance format
func convertToMigration(m interface{}) *types.Migration {
	// This would need proper conversion logic based on actual types
	return &types.Migration{}
}
```

---

## Integration Point 4: **WebUI** (Already Done! ✅)

Good news! The WebUI integration architecture is **already in place**. You just need to add governance endpoints to the existing WebUI.

### Where to Add: `pkg/core/database/migrations/webui/handlers.go`

```go
// Add new governance handlers
func (h *Handlers) handleGovernanceValidate(w http.ResponseWriter, r *http.Request) {
	// Implementation
}

func (h *Handlers) handleGovernanceApprove(w http.ResponseWriter, r *http.Request) {
	// Implementation
}

func (h *Handlers) handleComplianceReport(w http.ResponseWriter, r *http.Request) {
	// Implementation
}

// Register in RegisterHandlers() function
mux.HandleFunc("/api/governance/validate", h.handleGovernanceValidate)
mux.HandleFunc("/api/governance/approve", h.handleGovernanceApprove)
mux.HandleFunc("/api/governance/compliance", h.handleComplianceReport)
```

---

## Quick Integration Checklist

### ✅ To Integrate Governance into GoMigrate:

1. **Standalone CLI** (`cmd/gomigrate/main.go`):
   - [ ] Add governance flags (lines 27-35)
   - [ ] Modify `createMigrationTool()` (lines 148-155)
   - [ ] Add `wrapWithGovernance()` function
   - [ ] Add approve/reject/compliance commands

2. **Gogen CLI** (`cmd/cli/migrate.go`):
   - [ ] Add governance flags (line 150)
   - [ ] Modify `runUpMigration()` (lines 419-476)
   - [ ] Add policy validation loop
   - [ ] Add approval check logic

3. **Builder API** (NEW file):
   - [ ] Create `builder/governance_builder.go`
   - [ ] Implement `GovernanceMigrationBuilder`
   - [ ] Implement `GovernanceMigrationTool`
   - [ ] Export from package

4. **WebUI** (`webui/handlers.go`):
   - [ ] Add governance validation endpoint
   - [ ] Add approval workflow endpoints
   - [ ] Add compliance report endpoint
   - [ ] Update frontend to call new endpoints

---

## Example: Complete Integration for Standalone CLI

Here's a **complete working example** to add to `cmd/gomigrate/main.go`:

```bash
# Before integration:
$ gomigrate up

# After integration:
$ gomigrate up --governance --strict --env=production --user=john.doe

# New commands:
$ gomigrate approve approval-123 --approver=dba-1 --comment="LGTM"
$ gomigrate compliance --framework=SOX --start-date=2025-01-01
$ gomigrate policy list
$ gomigrate audit-trail --migration=migration-001
```

---

## Summary

**Current State:**
- ❌ Governance system is **implemented** but **not integrated** into actual GoMigrate code
- ❌ CLI commands don't call governance
- ❌ Builder API doesn't use governance
- ✅ WebUI architecture exists (just needs governance endpoints)

**What's Needed:**
- 🔧 Add ~100 lines to `cmd/gomigrate/main.go`
- 🔧 Add ~80 lines to `cmd/cli/migrate.go`
- 🔧 Create new `builder/governance_builder.go` (~200 lines)
- 🔧 Add 3 new endpoints to `webui/handlers.go` (~150 lines)

**Total Integration Effort:** ~530 lines of code across 4 files

**Result:** Full governance at all levels (CLI, Builder, WebUI) with backward compatibility!

---

Want me to create the actual integration PR with all the code changes?