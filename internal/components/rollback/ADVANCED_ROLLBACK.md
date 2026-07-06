# Advanced Rollback Capabilities

## Overview

This document describes the advanced rollback capabilities implemented in the migration system, addressing the following requirements:

1. **Selective Rollback** - Rollback specific migrations in the middle of the migration chain
2. **Impact Analysis** - Predict and analyze rollback impact before execution
3. **Rollback Testing** - Test rollback operations in staging before production

## Features

### 1. Selective Rollback

Selective rollback allows you to rollback a specific migration even if it's in the middle of the migration chain, with intelligent handling of dependencies.

#### Key Capabilities:
- Rollback specific migration by ID
- Automatic dependency detection and resolution
- Option to automatically rollback dependent migrations
- Force rollback even with unresolved dependencies
- Savepoint creation for safe rollback
- Automatic backup before rollback
- Dry-run mode for testing

#### Usage Example:

```go
// Create advanced rollback manager
advancedManager := rollback.NewDefaultAdvancedRollbackManager(
    baseRollbackManager,
    tracker,
    executor,
    logger,
    &rollback.AdvancedRollbackConfig{
        EnableSelectiveRollback: true,
        EnableSavepoints: true,
        DefaultDepth: 5,
        MaxDepth: 10,
    },
)

// Configure selective rollback options
options := &rollback.SelectiveRollbackOptions{
    Force:              false,        // Don't force if there are blockers
    RollbackDependents: true,         // Automatically rollback dependent migrations
    CreateSavepoint:    true,         // Create savepoint before rollback
    DryRun:             false,        // Execute actual rollback
    MaxDepth:           5,            // Maximum dependency depth
    CreateBackup:       true,         // Create backup before rollback
    Timeout:            5 * time.Minute,
}

// Execute selective rollback
result, err := advancedManager.RollbackSelective(ctx, "migration_id_here", options)

if err != nil {
    log.Fatal(err)
}

if result.Success {
    fmt.Printf("Successfully rolled back %d migrations\n", len(result.RolledBackMigrations))
    fmt.Printf("Backup location: %s\n", result.BackupLocation)
    fmt.Printf("Duration: %s\n", result.Duration)
} else {
    fmt.Printf("Rollback failed with errors:\n")
    for _, err := range result.Errors {
        fmt.Printf("  - %s\n", err)
    }
}
```

### 2. Impact Analysis

Impact analysis provides detailed information about the potential consequences of a rollback operation before you execute it.

#### Key Capabilities:
- Identify all affected migrations
- Detect affected database objects (tables, columns, indexes, constraints)
- Assess data loss risk
- Estimate data loss volume
- Detect rollback conflicts
- Calculate estimated rollback duration
- Generate safety recommendations
- Determine if rollback can proceed safely

#### Impact Levels:
- **Minimal** - Safe rollback with no data loss risk
- **Low** - Minor schema changes, low risk
- **Medium** - Schema modifications, moderate risk
- **High** - Data loss possible, high risk
- **Critical** - Major data loss, critical risk

#### Usage Example:

```go
// Analyze rollback impact
analysis, err := advancedManager.AnalyzeRollbackImpact(ctx, []string{"migration_001", "migration_002"})

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Impact Analysis Report\n")
fmt.Printf("======================\n")
fmt.Printf("Impact Level: %s\n", analysis.ImpactLevel)
fmt.Printf("Can Proceed Safely: %v\n", analysis.CanProceedSafely)
fmt.Printf("Requires Manual Intervention: %v\n", analysis.RequiresManual)
fmt.Printf("Data Loss Risk: %v\n", analysis.DataLossRisk)
fmt.Printf("Estimated Duration: %s\n", analysis.EstimatedDuration)

if analysis.EstimatedDataLoss != nil {
    fmt.Printf("\nEstimated Data Loss:\n")
    fmt.Printf("  Total Rows: %d\n", analysis.EstimatedDataLoss.TotalEstimate)
    fmt.Printf("  Tables Affected: %v\n", analysis.EstimatedDataLoss.TablesAffected)
}

fmt.Printf("\nAffected Migrations: %d\n", len(analysis.AffectedMigrations))
for _, impact := range analysis.AffectedMigrations {
    fmt.Printf("  - %s (Level: %s, Can Rollback: %v)\n",
        impact.Migration.ID, impact.ImpactLevel, impact.CanRollback)
}

if len(analysis.Conflicts) > 0 {
    fmt.Printf("\nConflicts Detected: %d\n", len(analysis.Conflicts))
    for _, conflict := range analysis.Conflicts {
        fmt.Printf("  - [%s] %s: %s\n",
            conflict.Severity, conflict.Type, conflict.Description)
        if conflict.CanAutoResolve {
            fmt.Printf("    Resolution: %s (auto-resolvable)\n", conflict.Resolution)
        } else {
            fmt.Printf("    Resolution: %s (manual intervention required)\n", conflict.Resolution)
        }
    }
}

if len(analysis.Recommendations) > 0 {
    fmt.Printf("\nRecommendations:\n")
    for _, rec := range analysis.Recommendations {
        fmt.Printf("  - %s\n", rec)
    }
}

if len(analysis.Warnings) > 0 {
    fmt.Printf("\nWarnings:\n")
    for _, warning := range analysis.Warnings {
        fmt.Printf("  - %s\n", warning)
    }
}
```

### 3. Rollback Testing

Rollback testing allows you to validate rollback operations in a safe staging environment before executing them in production.

#### Key Capabilities:
- Test rollback in designated environments (staging, test)
- Multiple test cases per migration
- Automated validation assertions
- Coverage metrics
- Pass/fail reporting
- Recommendation generation
- Timeout protection

#### Test Cases Include:
1. Rollback SQL existence check
2. Rollback capability verification
3. Rollback SQL syntax validation
4. Dependency check
5. Impact assessment

#### Usage Example:

```go
// Test rollback in staging environment
result, err := advancedManager.TestRollback(
    ctx,
    []string{"migration_001", "migration_002"},
    "staging", // Environment must be in configured TestEnvironments
)

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Rollback Test Results\n")
fmt.Printf("=====================\n")
fmt.Printf("Environment: %s\n", result.Environment)
fmt.Printf("Success: %v\n", result.Success)
fmt.Printf("Total Tests: %d\n", result.TotalTests)
fmt.Printf("Passed: %d\n", result.PassedTests)
fmt.Printf("Failed: %d\n", result.FailedTests)
fmt.Printf("Skipped: %d\n", result.SkippedTests)
fmt.Printf("Coverage: %.2f%%\n", result.Coverage)
fmt.Printf("Duration: %s\n", result.Duration)
fmt.Printf("\nRecommendation: %s\n", result.Recommendation)

if len(result.TestCases) > 0 {
    fmt.Printf("\nTest Cases:\n")
    for _, tc := range result.TestCases {
        status := "✓"
        if tc.Status == constants.TestStatusFailed {
            status = "✗"
        }
        fmt.Printf("  %s %s (%s)\n", status, tc.Name, tc.Duration)
        if tc.Error != "" {
            fmt.Printf("    Error: %s\n", tc.Error)
        }
        if len(tc.Assertions) > 0 {
            fmt.Printf("    Assertions:\n")
            for _, assertion := range tc.Assertions {
                fmt.Printf("      - %s\n", assertion)
            }
        }
    }
}

if !result.Success {
    fmt.Printf("\nErrors:\n")
    for _, err := range result.Errors {
        fmt.Printf("  - %s\n", err)
    }
}
```

### 4. Dependency Graph Visualization

Build and analyze the complete dependency graph for migrations to understand rollback implications.

#### Key Capabilities:
- Build complete dependency graph
- Calculate dependency levels
- Detect circular dependencies
- Identify blocking dependencies
- Visualize migration relationships

#### Usage Example:

```go
// Get dependency graph for a migration
graph, err := advancedManager.GetRollbackDependencyGraph(ctx, "migration_001")

if err != nil {
    log.Fatal(err)
}

fmt.Printf("Dependency Graph Analysis\n")
fmt.Printf("=========================\n")
fmt.Printf("Total Nodes: %d\n", graph.TotalNodes)
fmt.Printf("Total Edges: %d\n", len(graph.Edges))
fmt.Printf("Max Depth: %d\n", graph.MaxDepth)
fmt.Printf("Has Cycles: %v\n", graph.HasCycles)

if graph.HasCycles {
    fmt.Printf("\nCyclic Paths Detected:\n")
    for _, path := range graph.CyclicPaths {
        fmt.Printf("  - %v\n", path)
    }
}

fmt.Printf("\nDependency Levels:\n")
for level, nodeIDs := range graph.Levels {
    fmt.Printf("  Level %d: %v\n", level, nodeIDs)
}

fmt.Printf("\nNode Details:\n")
for nodeID, node := range graph.Nodes {
    fmt.Printf("  %s (Level %d, Rollbackable: %v)\n",
        nodeID, node.Level, node.IsRollbackable)
    if len(node.Dependencies) > 0 {
        fmt.Printf("    Dependencies: %v\n", node.Dependencies)
    }
    if len(node.Dependents) > 0 {
        fmt.Printf("    Dependents: %v\n", node.Dependents)
    }
}

fmt.Printf("\nEdges:\n")
for _, edge := range graph.Edges {
    blockingStr := ""
    if edge.IsBlocking {
        blockingStr = " (blocking)"
    }
    fmt.Printf("  %s -> %s [%s]%s\n",
        edge.From, edge.To, edge.EdgeType, blockingStr)
}
```

## Configuration

### AdvancedRollbackConfig

```go
type AdvancedRollbackConfig struct {
    // Enable selective rollback
    EnableSelectiveRollback bool

    // Enable impact analysis
    EnableImpactAnalysis bool

    // Enable rollback testing
    EnableRollbackTesting bool

    // Default depth for dependency analysis
    DefaultDepth int

    // Maximum depth for dependency analysis
    MaxDepth int

    // Enable automatic conflict resolution
    EnableAutoConflictResolution bool

    // Enable savepoints
    EnableSavepoints bool

    // Testing environment names
    TestEnvironments []string

    // Timeout for impact analysis
    ImpactAnalysisTimeout time.Duration

    // Timeout for rollback testing
    TestTimeout time.Duration
}
```

### Default Configuration

```go
config := &AdvancedRollbackConfig{
    EnableSelectiveRollback:      true,
    EnableImpactAnalysis:         true,
    EnableRollbackTesting:        true,
    DefaultDepth:                 5,
    MaxDepth:                     10,
    EnableAutoConflictResolution: false, // Conservative default
    EnableSavepoints:             true,
    TestEnvironments:             []string{"staging", "test"},
    ImpactAnalysisTimeout:        5 * time.Minute,
    TestTimeout:                  10 * time.Minute,
}
```

## Best Practices

### 1. Always Analyze Impact First

Before executing a selective rollback, always run impact analysis:

```go
// Step 1: Analyze impact
analysis, err := advancedManager.AnalyzeRollbackImpact(ctx, migrationIDs)
if err != nil || !analysis.CanProceedSafely {
    // Review warnings and recommendations
    log.Println("Rollback may not be safe, review analysis carefully")
    return
}

// Step 2: Test in staging
testResult, err := advancedManager.TestRollback(ctx, migrationIDs, "staging")
if err != nil || !testResult.Success {
    log.Println("Rollback test failed, do not proceed to production")
    return
}

// Step 3: Execute selective rollback
result, err := advancedManager.RollbackSelective(ctx, migrationID, options)
```

### 2. Use Dry Run for Validation

Always use dry-run mode first to validate the rollback plan:

```go
// Test with dry run
options := &SelectiveRollbackOptions{
    DryRun: true,
    RollbackDependents: true,
}
dryRunResult, _ := advancedManager.RollbackSelective(ctx, migrationID, options)

// Review dry run results
if dryRunResult.Success {
    // Execute actual rollback
    options.DryRun = false
    actualResult, _ := advancedManager.RollbackSelective(ctx, migrationID, options)
}
```

### 3. Handle Dependencies Carefully

When rolling back migrations with dependents, carefully consider the options:

```go
// Option 1: Rollback with dependents (safest)
options := &SelectiveRollbackOptions{
    RollbackDependents: true,  // Automatically rollback all dependents
}

// Option 2: Force rollback (risky)
options := &SelectiveRollbackOptions{
    Force: true,  // Force rollback even with blocking dependents
    // WARNING: This can leave the database in an inconsistent state
}

// Option 3: Fail if there are dependents (safest)
options := &SelectiveRollbackOptions{
    RollbackDependents: false,
    Force: false,
    // Will fail if there are dependent migrations
}
```

### 4. Monitor and Log

Always enable logging for rollback operations:

```go
logger := observability.NewComponentLogger("rollback")
advancedManager := rollback.NewDefaultAdvancedRollbackManager(
    baseManager,
    tracker,
    executor,
    logger,  // Logger will capture all rollback operations
    config,
)
```

### 5. Test in Non-Production First

Always test rollback operations in staging before production:

```go
environments := []string{"test", "staging", "production"}

for _, env := range environments[:2] {  // Test in test and staging only
    result, err := advancedManager.TestRollback(ctx, migrationIDs, env)
    if err != nil || !result.Success {
        log.Printf("Rollback test failed in %s, aborting", env)
        return
    }
}

// Only proceed to production if all tests pass
```

## Error Handling

The advanced rollback system uses structured error handling with detailed context:

```go
result, err := advancedManager.RollbackSelective(ctx, migrationID, options)

if err != nil {
    // Handle initialization or configuration errors
    log.Printf("Rollback failed with error: %v", err)
    return
}

if !result.Success {
    // Handle execution errors
    log.Println("Rollback execution failed:")
    for _, errMsg := range result.Errors {
        log.Printf("  - %s", errMsg)
    }

    // Review warnings
    for _, warning := range result.Warnings {
        log.Printf("  Warning: %s", warning)
    }
}

// Check impact analysis if available
if result.ImpactAnalysis != nil {
    if result.ImpactAnalysis.ImpactLevel >= constants.ImpactLevelHigh {
        log.Println("High impact rollback - ensure backup is available")
    }
}
```

## Integration with Existing System

The advanced rollback manager integrates seamlessly with the existing rollback infrastructure:

```go
// Create base rollback manager
baseManager := rollback.NewDefaultRollbackManager(
    tracker,
    executor,
    validator,
    baseConfig,
)

// Create advanced rollback manager
advancedManager := rollback.NewDefaultAdvancedRollbackManager(
    baseManager,  // Uses base manager for actual rollback execution
    tracker,
    executor,
    logger,
    advancedConfig,
)

// Link them together
baseManager.SetAdvancedManager(advancedManager)

// Access advanced features through base manager
if adv := baseManager.GetAdvancedManager(); adv != nil {
    analysis, _ := adv.AnalyzeRollbackImpact(ctx, migrationIDs)
}
```

## Troubleshooting

### Issue: "Impact analysis indicates rollback cannot proceed safely"

**Solution**: Review the impact analysis output, check warnings and recommendations. If necessary, use the Force option or resolve conflicts manually.

### Issue: "Migration has blocking dependents"

**Solution**: Either:
1. Use `RollbackDependents: true` to automatically rollback dependent migrations
2. Use `Force: true` to force rollback (risky)
3. Manually rollback dependent migrations first

### Issue: "Rollback test failed in staging"

**Solution**: Review test case failures, validate rollback SQL, check database state in staging, resolve issues before attempting production rollback.

### Issue: "Data loss risk detected"

**Solution**: Ensure backup is available and tested, review estimated data loss, verify backup/restore procedures, consider alternative approaches if data loss is unacceptable.

## Advanced Usage Patterns

### Pattern 1: Progressive Rollback

Roll back migrations progressively with validation at each step:

```go
for i, migrationID := range migrationIDsToRollback {
    // Analyze impact
    analysis, _ := advancedManager.AnalyzeRollbackImpact(ctx, []string{migrationID})

    if analysis.ImpactLevel >= constants.ImpactLevelHigh {
        log.Printf("High impact detected for %s, requesting manual approval", migrationID)
        // Wait for approval
        if !getApproval() {
            break
        }
    }

    // Execute rollback
    result, _ := advancedManager.RollbackSelective(ctx, migrationID, options)

    if !result.Success {
        log.Printf("Rollback failed at step %d, stopping", i+1)
        break
    }
}
```

### Pattern 2: Safe Rollback with Verification

Implement multi-stage rollback with verification:

```go
// Stage 1: Impact Analysis
analysis, _ := advancedManager.AnalyzeRollbackImpact(ctx, migrationIDs)
if !analysis.CanProceedSafely {
    return errors.New("cannot proceed safely")
}

// Stage 2: Test in Staging
testResult, _ := advancedManager.TestRollback(ctx, migrationIDs, "staging")
if testResult.Coverage < 100 {
    return errors.New("test coverage insufficient")
}

// Stage 3: Dry Run
dryRunOptions := &SelectiveRollbackOptions{DryRun: true}
dryRunResult, _ := advancedManager.RollbackSelective(ctx, migrationID, dryRunOptions)

// Stage 4: Actual Rollback
actualOptions := &SelectiveRollbackOptions{
    CreateBackup: true,
    CreateSavepoint: true,
}
actualResult, _ := advancedManager.RollbackSelective(ctx, migrationID, actualOptions)
```

### Pattern 3: Dependency-Aware Rollback

Build and analyze dependency graph before rollback:

```go
// Build dependency graph
graph, _ := advancedManager.GetRollbackDependencyGraph(ctx, migrationID)

if graph.HasCycles {
    log.Println("Circular dependencies detected, manual resolution required")
    return
}

// Rollback by dependency levels (deepest first)
for level := graph.MaxDepth; level >= 0; level-- {
    for _, nodeID := range graph.Levels[level] {
        result, _ := advancedManager.RollbackSelective(ctx, nodeID, options)
        if !result.Success {
            log.Printf("Failed to rollback level %d, stopping", level)
            return
        }
    }
}
```

## API Reference

See [advanced_rollback.go](./advanced_rollback.go) for complete API documentation and type definitions.

## Contributing

When adding new features to the advanced rollback system:

1. Follow existing patterns for error handling
2. Use constants from `constants` package
3. Add comprehensive logging via ComponentLogger
4. Write unit tests for new functionality
5. Update this documentation

## License

Part of the go-gen migration framework.
