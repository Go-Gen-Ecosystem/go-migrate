# Migration Governance System

Enterprise-grade policy-based governance for database migrations with approval workflows, role-based access control, and compliance reporting.

## Features

### 1. Policy-Based Governance
Enforce organizational rules on migrations before execution:

- **No DROP TABLE in Production** - Prevents destructive table drops in production
- **Require Approval for Destructive Operations** - Forces approval workflow for DROP, DELETE, TRUNCATE, ALTER
- **Require Rollback SQL** - Ensures destructive operations have rollback capability
- **No Hardcoded Credentials** - Blocks migrations with hardcoded passwords/secrets
- **Require Metadata** - Enforces description and author fields
- **Complexity Limits** - Prevents overly complex migrations

### 2. Approval Workflows
Multi-stage approval process for risky migrations:

- **Configurable Approval Requirements** - Minimum approvals, require all approvers, timeout
- **Approval Status Tracking** - Pending, approved, rejected, expired, canceled
- **Separation of Duties** - Prevents self-approval
- **Notification System** - Alerts for approval requests and decisions
- **Expiration Handling** - Automatic cleanup of expired requests

### 3. Role-Based Access Control (RBAC)
Fine-grained permission system:

**Default Roles:**
- **Developer** - Can create and view migrations
- **DBA** - Can execute, approve, and rollback migrations
- **Manager** - Can approve/reject migrations
- **Admin** - Full permissions
- **Auditor** - Read-only for compliance

**Permissions:**
- `migration:execute` - Execute migrations
- `migration:rollback` - Rollback migrations
- `migration:approve` - Approve migrations
- `migration:reject` - Reject migrations
- `migration:view` - View migrations
- `migration:create` - Create migrations
- `migration:delete` - Delete migrations
- `policy:manage` - Manage policies
- `role:manage` - Manage roles
- `audit:view` - View audit logs

### 4. Compliance Reporting
Generate compliance reports for regulatory frameworks:

**Supported Frameworks:**
- **SOX** (Sarbanes-Oxley) - Change control, separation of duties, audit trail
- **HIPAA** - Access control, audit controls, data integrity
- **GDPR** - Data protection, privacy by design
- **PCI-DSS** - Secure development, change tracking
- **ISO 27001** - Information security management

**Report Features:**
- Audit trail with complete history
- Policy violation tracking
- Compliance score calculation (0-100)
- Framework-specific findings
- Actionable recommendations
- Automatic retention management

## Usage

### Basic Setup

```go
import (
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/governance"
)

// Create governance configuration
config := governance.GovernanceConfig{
    Environment: "production",
    PolicyEngineConfig: governance.PolicyEngineConfig{
        StrictMode: true,
        RequireApproval: true,
    },
    ApprovalConfig: governance.ApprovalWorkflowConfig{
        RequireApproval: true,
        MinimumApprovals: 2,
        ApprovalTimeout: 24 * time.Hour,
        AllowSelfApproval: false,
    },
    ComplianceConfig: governance.ComplianceConfig{
        Framework: governance.ComplianceFrameworkSOX,
        OutputDir: "./compliance-reports",
        RetentionPeriod: 7 * 365 * 24 * time.Hour, // 7 years for SOX
        AutoGenerate: true,
    },
    Enabled: true,
}

// Create governance manager
manager, err := governance.NewGovernanceManager(config)
if err != nil {
    log.Fatal(err)
}
```

### Policy Validation

```go
// Validate migration against policies
result, err := manager.ValidateMigration(ctx, migration)
if err != nil {
    log.Fatal(err)
}

if !result.Passed {
    for _, violation := range result.Violations {
        fmt.Printf("Policy %s: %s\n", violation.PolicyName, violation.Message)
        fmt.Printf("  Suggestion: %s\n", violation.Suggestion)
    }
}

if result.Action == governance.PolicyActionBlock {
    log.Fatal("Migration blocked by policy")
}
```

### Approval Workflow

```go
// Request approval for a risky migration
approvers := []string{"dba-1", "manager-1"}
request, err := manager.RequestMigrationApproval(
    ctx,
    migration,
    "developer-1",
    approvers,
    policyResult,
)

// DBA approves
err = manager.ApproveMigration(ctx, request.ID, "dba-1", "John DBA", "Looks good")

// Manager approves
err = manager.ApproveMigration(ctx, request.ID, "manager-1", "Jane Manager", "Approved")

// Check if approved
approved, err := manager.CheckMigrationApproval(ctx, migration.ID)
```

### RBAC Management

```go
rbac := manager.GetRBACManager()

// Add user
user := &governance.User{
    ID: "user-1",
    Username: "john.doe",
    Email: "john.doe@example.com",
    Roles: []string{"developer"},
    Active: true,
}
err := rbac.AddUser(user)

// Assign role
err = rbac.AssignRole("user-1", "dba")

// Check permissions
canExecute, err := rbac.CanExecute(ctx, "user-1")
if !canExecute {
    log.Fatal("User cannot execute migrations")
}
```

### Compliance Reporting

```go
// Generate compliance report
report, err := manager.GenerateComplianceReport(
    ctx,
    time.Now().AddDate(0, -1, 0), // Last month
    time.Now(),
    "auditor-1",
)

fmt.Printf("Compliance Score: %.2f\n", report.ComplianceScore)
fmt.Printf("Total Migrations: %d\n", report.TotalMigrations)
fmt.Printf("Policy Violations: %d\n", report.PolicyViolations)

for _, finding := range report.Findings {
    fmt.Printf("Finding [%s]: %s\n", finding.Severity, finding.Description)
    fmt.Printf("  Remediation: %s\n", finding.Remediation)
}
```

### Integration with Migration Pipeline

```go
// Wrap executor with governance
executor := executor.NewDatabaseMigrationExecutor(db, &config.Migration)
governanceExecutor := governance.NewGovernanceExecutorDecorator(
    executor,
    manager,
    "user-1",
    "John Doe",
)

// Wrap validator with governance
validator := validator.NewCompositeMigrationValidator(db, &config, tracker)
governanceValidator := governance.NewGovernanceValidatorDecorator(
    validator,
    manager,
)

// Use decorated components in engine
// Governance checks are now automatically enforced
```

## Architecture

### Components

1. **PolicyEngine** - Evaluates migrations against organizational policies
2. **ApprovalWorkflow** - Manages multi-stage approval processes
3. **RBACManager** - Controls access based on roles and permissions
4. **ComplianceReporter** - Generates audit and compliance reports
5. **SeparationOfDuties** - Enforces creator-approver separation
6. **GovernanceManager** - Coordinates all governance components

### Integration Points

- **Pipeline Decorators** - Intercept execution/validation
- **Executor Wrapper** - Adds governance checks before execution
- **Validator Wrapper** - Adds policy validation
- **Audit Store** - Records all operations for compliance

### Storage

- **InMemoryApprovalStore** - For development/testing
- **InMemoryAuditStore** - For development/testing
- **DatabaseApprovalStore** - Production-ready (requires implementation)
- **DatabaseAuditStore** - Production-ready (requires implementation)

## Policy Examples

### Custom Policy

```go
type CustomPolicy struct {
    governance.BasePolicy
}

func (p *CustomPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*governance.PolicyViolation, error) {
    // Custom logic
    if strings.Contains(migration.Script, "FORBIDDEN_KEYWORD") {
        return &governance.PolicyViolation{
            PolicyName: p.Name,
            Severity: governance.PolicySeverityError,
            Message: "Forbidden keyword detected",
            MigrationID: migration.ID,
            Suggestion: "Remove forbidden keyword",
            Timestamp: time.Now(),
        }, nil
    }
    return nil, nil
}

// Add to engine
engine.AddPolicy(&CustomPolicy{
    BasePolicy: governance.BasePolicy{
        Name: "custom_policy",
        Description: "Custom policy example",
        Severity: governance.PolicySeverityError,
        Action: governance.PolicyActionBlock,
        Enabled: true,
    },
})
```

## Best Practices

1. **Enable Strict Mode in Production** - Convert warnings to errors
2. **Require Approval for Risky Operations** - Use multi-stage approval
3. **Enforce Separation of Duties** - Disable self-approval
4. **Regular Compliance Reports** - Generate monthly reports
5. **Audit Retention** - Keep audit trail for 7+ years for SOX
6. **Role Assignment** - Follow principle of least privilege
7. **Policy Customization** - Add organization-specific policies
8. **Notification Setup** - Configure approval notifications

## Compliance Frameworks

### SOX Compliance
- Separation of duties enforced
- Complete audit trail maintained
- Change control through approvals
- Automated compliance reporting

### HIPAA Compliance
- Access control through RBAC
- Audit controls for PHI access
- Data integrity policies
- Unauthorized access detection

### GDPR Compliance
- Data protection policies
- Privacy by design
- Audit trail for data changes
- Right to erasure support

## Testing

Run tests:
```bash
go test ./pkg/core/database/migrations/internal/components/governance/...
```

## Configuration

See `GovernanceConfig` for full configuration options:
- Policy engine settings
- Approval workflow settings
- Compliance framework selection
- Audit retention policies
- RBAC configuration

## Future Enhancements

- Email/Slack notifications
- Database-backed stores
- Advanced policy DSL
- Multi-framework compliance
- Real-time compliance dashboards
- Integration with external approval systems
