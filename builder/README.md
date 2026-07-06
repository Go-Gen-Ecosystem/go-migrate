# Migration Builder API with Governance

This package provides a fluent builder API for creating migration tools with optional governance integration.

## Basic Usage

### Without Governance

```go
import "github.com/gogen-framework/gogen/pkg/core/database/migrations/builder"

tool, err := builder.NewGovernanceToolBuilder().
    WithConfigPath(".gomigrate.yaml").
    Build()
if err != nil {
    panic(err)
}
defer tool.Close()

// Use the tool
ctx := context.Background()
err = tool.Migrate(ctx, "", false)
```

### With Governance

```go
import "github.com/gogen-framework/gogen/pkg/core/database/migrations/builder"

tool, err := builder.NewGovernanceToolBuilder().
    WithConfigPath(".gomigrate.yaml").
    WithGovernance().
    WithEnvironment("production").
    WithStrictMode().
    WithApprovalWorkflow(2).
    WithComplianceFramework("SOX").
    WithUser("john.doe", "John Doe").
    Build()
if err != nil {
    panic(err)
}
defer tool.Close()

// Migrations will now be validated against governance policies
ctx := context.Background()
err = tool.Migrate(ctx, "", false)
```

## Configuration Options

### `WithConfigPath(path string)`
Sets the path to the GoMigrate configuration file (default: `.gomigrate.yaml`)

### `WithGovernance()`
Enables governance with default settings

### `WithEnvironment(env string)`
Sets the environment: `development`, `staging`, `production`

### `WithStrictMode()`
Enables strict mode where policy warnings become blocking errors

### `WithApprovalWorkflow(minApprovals int)`
Enables approval workflow requiring a minimum number of approvals

### `WithComplianceFramework(framework string)`
Sets the compliance framework for reporting:
- `SOX` - Sarbanes-Oxley
- `HIPAA` - Health Insurance Portability and Accountability Act
- `GDPR` - General Data Protection Regulation
- `PCI-DSS` - Payment Card Industry Data Security Standard
- `ISO` - ISO 27001

### `WithUser(userID, userName string)`
Sets the current user information for audit trails

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/gogen-framework/gogen/pkg/core/database/migrations/builder"
)

func main() {
    // Build migration tool with governance
    tool, err := builder.NewGovernanceToolBuilder().
        WithConfigPath(".gomigrate.yaml").
        WithGovernance().
        WithEnvironment("production").
        WithStrictMode().
        WithApprovalWorkflow(2).
        WithComplianceFramework("SOX").
        WithUser("admin", "System Administrator").
        Build()
    if err != nil {
        log.Fatalf("Failed to build migration tool: %v", err)
    }
    defer tool.Close()

    ctx := context.Background()

    // Check status
    status, err := tool.Status(ctx)
    if err != nil {
        log.Fatalf("Failed to get status: %v", err)
    }
    fmt.Printf("Pending migrations: %d\n", status.PendingCount)

    // Run migrations (with governance checks)
    if err := tool.Migrate(ctx, "", false); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }

    fmt.Println("Migration completed successfully")
}
```

## Governance Features

When governance is enabled, the migration tool will:

1. **Policy Validation**: Check migrations against 6 default policies
   - No DROP TABLE in production
   - Require rollback SQL for destructive operations
   - No hardcoded credentials
   - Require migration metadata
   - Complexity limits
   - Destructive operation approvals

2. **RBAC Permissions**: Verify user has required permissions

3. **Approval Workflow**: Require approvals for risky operations

4. **Audit Trail**: Log all operations for compliance

5. **Compliance Reporting**: Generate reports for regulatory frameworks

## Integration Points

This builder API can be used:
- In custom Go applications
- CI/CD pipelines
- Infrastructure as Code tools
- Automated deployment scripts
- Testing frameworks

## See Also

- [Governance Documentation](../governance/README.md)
- [CLI Integration](../../cmd/gomigrate/main.go)
- [Integration Guide](../governance/GOVERNANCE_INTEGRATION_GUIDE.md)
