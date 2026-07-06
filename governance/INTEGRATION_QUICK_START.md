# Governance Integration - Quick Start Guide

## TL;DR - How to Use Governance

### 3 Ways to Use the Governance System

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│  1. CLI (Command Line)     2. Builder API    3. WebUI      │
│     $ gomigrate migrate       tool.Migrate()    Dashboard   │
│       --governance                                          │
│                                                             │
└──────────────────┬──────────────┬───────────────┬───────────┘
                   │              │               │
                   ▼              ▼               ▼
              ┌────────────────────────────────────┐
              │      Governance Manager            │
              │   (Automatic Policy Enforcement)   │
              └────────────────────────────────────┘
```

---

## Option 1: CLI Integration (Easiest)

### Quick Start

```bash
# Step 1: Enable governance for your migration
gomigrate migrate --governance --env=production --user=john.doe

# Step 2: If approval needed
gomigrate approvals list --status=pending
gomigrate approve approval-123 --approver=dba-1 --comment="LGTM"

# Step 3: Generate compliance report
gomigrate compliance --framework=SOX --start-date=2025-01-01
```

### Full Example

```bash
# 1. Create migration file
cat > migrations/001_add_users.sql <<EOF
-- Description: Add users table
-- Author: john.doe
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
EOF

# 2. Run with governance in production
gomigrate migrate \
  --governance \
  --env=production \
  --strict \
  --require-approval \
  --min-approvals=2 \
  --compliance=SOX \
  --user=john.doe

# Output:
# Found 1 pending migration(s)
# 🔒 Running governance checks...
# ✓ All governance checks passed
# 🚀 Executing migrations...
# ✓ Migration completed successfully
# 📊 Compliance Score: 95.00/100

# 3. View audit trail
gomigrate audit-trail --migration=001_add_users

# 4. Generate monthly compliance report
gomigrate compliance \
  --framework=SOX \
  --start-date=2025-01-01 \
  --end-date=2025-01-31 \
  --output=./reports/sox-january-2025.pdf
```

---

## Option 2: Builder API Integration (Most Flexible)

### Quick Start

```go
package main

import (
    "context"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/builder"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/governance"
)

func main() {
    // Create migration tool with governance
    tool, _ := builder.NewMigrationBuilder().
        WithGovernance(governance.GovernanceConfig{
            Environment: "production",
            PolicyEngineConfig: governance.PolicyEngineConfig{
                StrictMode: true,
            },
        }).
        WithUser("api-service", "API Service").
        Build()

    // Execute migrations - governance is automatic!
    tool.Migrate(context.Background(), migrations)
}
```

### Full Example with All Features

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/gogen-framework/gogen/pkg/core/database/migrations/builder"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/governance"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

func main() {
    ctx := context.Background()

    // 1. Configure governance
    govConfig := governance.GovernanceConfig{
        Environment: "production",
        PolicyEngineConfig: governance.PolicyEngineConfig{
            Environment:     "production",
            StrictMode:      true,
            RequireApproval: true,
        },
        ApprovalConfig: governance.ApprovalWorkflowConfig{
            RequireApproval:     true,
            MinimumApprovals:    2,
            AllowSelfApproval:   false,
            ApprovalTimeout:     24 * time.Hour,
        },
        ComplianceConfig: governance.ComplianceConfig{
            Framework:       governance.ComplianceFrameworkSOX,
            RetentionPeriod: 7 * 365 * 24 * time.Hour,
        },
    }

    // 2. Create migration tool
    tool, err := builder.NewMigrationBuilder().
        WithGovernance(govConfig).
        WithUser("developer-1", "John Developer").
        Build()

    if err != nil {
        panic(err)
    }

    // 3. Define migration
    migration := &types.Migration{
        ID:          "migration-001",
        Version:     "1.0.0",
        Description: "Add user preferences",
        Script:      "ALTER TABLE users ADD COLUMN preferences JSON;",
        Author:      "developer-1",
        CanRollback: true,
        RollbackSQL: "ALTER TABLE users DROP COLUMN preferences;",
    }

    // 4. Validate (automatic, but can check explicitly)
    result, _ := tool.ValidateMigration(ctx, migration)

    if result.RequiresApproval {
        // 5. Request approval
        approvers := []string{"dba-1", "manager-1"}
        request, _ := tool.RequestApproval(ctx, migration, approvers)

        fmt.Printf("Approval requested: %s\n", request.ID)
        fmt.Println("Waiting for approvals...")

        // In production, you'd poll or use webhooks
        // For demo, simulate approvals
        time.Sleep(1 * time.Second)
    }

    // 6. Execute (governance checks automatic)
    err = tool.Migrate(ctx, []*types.Migration{migration})
    if err != nil {
        panic(err)
    }

    fmt.Println("✓ Migration completed successfully")

    // 7. Generate compliance report
    report, _ := tool.GenerateComplianceReport(
        ctx,
        time.Now().AddDate(0, -1, 0), // Last month
        time.Now(),
    )

    fmt.Printf("📊 Compliance Score: %.2f/100\n", report.ComplianceScore)
    fmt.Printf("   Total Migrations: %d\n", report.TotalMigrations)
    fmt.Printf("   Policy Violations: %d\n", report.PolicyViolations)
}
```

---

## Option 3: WebUI Integration (Most User-Friendly)

### Quick Start - Backend Setup

```go
// main.go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/webui/api"
    "github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/governance"
)

func main() {
    // Initialize governance
    govConfig := governance.GovernanceConfig{/* ... */}
    governanceManager, _ := governance.NewGovernanceManager(govConfig)

    // Setup API
    router := gin.Default()
    governanceAPI := api.NewGovernanceAPI(governanceManager)
    governanceAPI.RegisterRoutes(router)

    // Start server
    router.Run(":8080")
}
```

### Quick Start - Frontend Usage

```typescript
// MigrationPage.tsx
import React, { useState } from 'react';
import { GovernanceService } from './services/governance';

function MigrationPage() {
  const [violations, setViolations] = useState([]);
  const governanceService = new GovernanceService();

  const handleMigrationSubmit = async (script: string) => {
    // Validate against policies
    const result = await governanceService.validateMigration('mig-001', script);

    if (!result.passed) {
      setViolations(result.violations);

      if (result.action === 'block') {
        alert('Migration blocked by policy!');
        return;
      }

      if (result.requires_approval) {
        // Request approval
        const request = await governanceService.requestApproval(
          'mig-001',
          ['dba-1', 'manager-1']
        );

        alert(`Approval requested: ${request.id}`);
        return;
      }
    }

    // Execute migration
    executeMigration(script);
  };

  return (
    <div>
      <h1>Migration Dashboard</h1>

      {/* Migration form */}
      <textarea onChange={(e) => setScript(e.target.value)} />
      <button onClick={() => handleMigrationSubmit(script)}>
        Run Migration
      </button>

      {/* Violations display */}
      {violations.map(v => (
        <div className={`alert alert-${v.severity}`}>
          <strong>{v.policy_name}</strong>: {v.message}
        </div>
      ))}
    </div>
  );
}
```

---

## Common Use Cases

### Use Case 1: Prevent DROP TABLE in Production

```bash
# Developer tries dangerous migration
$ cat > migrations/002_cleanup.sql <<EOF
DROP TABLE old_users;
EOF

$ gomigrate migrate --governance --env=production --user=dev-1

# Output:
# ⚠️  Migration 002_cleanup has policy violations:
#   - [error] no_drop_table_production: DROP TABLE operations are not allowed in production
#     💡 Use a different approach like marking records as deleted
# Error: migration blocked by policy
```

### Use Case 2: Require Approval for Risky Operations

```bash
# Developer creates migration with DELETE
$ cat > migrations/003_delete_inactive.sql <<EOF
DELETE FROM users WHERE last_login < NOW() - INTERVAL '2 years';
EOF

$ gomigrate migrate --governance --env=production --user=dev-1

# Output:
# ⚠️  Migration 003_delete_inactive has policy violations:
#   - [warning] destructive_operation_approval: Destructive operation 'DELETE' detected
#     💡 Ensure you have proper approval and backup before proceeding
# 📋 This migration requires approval. Use 'gomigrate approve' command.

# DBA approves
$ gomigrate approve approval-456 --approver=dba-1 --comment="Reviewed backup plan"

# Manager approves
$ gomigrate approve approval-456 --approver=mgr-1 --comment="Business approved"

# Now it can execute
$ gomigrate migrate --governance --env=production --user=dba-1
# ✓ Migration completed successfully
```

### Use Case 3: Monthly Compliance Audit

```bash
# Generate SOX compliance report for auditors
$ gomigrate compliance \
  --framework=SOX \
  --start-date=2025-01-01 \
  --end-date=2025-01-31 \
  --output=./audits/sox-jan-2025.json

# Output:
# SOX Compliance Report
# =====================================
# Period: 2025-01-01 to 2025-01-31
# Compliance Score: 92.50/100
#
# Migrations:
#   Total: 45
#   Approved: 43
#   Rejected: 2
#   Policy Violations: 3
#
# Findings:
#   1. [medium] Change Control: Low approval rate (82.2%)
#      Remediation: Review change control processes
#
# Recommendations:
#   1. Implement stricter approval workflows for high-risk migrations
#   2. Conduct compliance training for DBAs and developers
```

---

## Configuration Examples

### Development Environment

```yaml
# config-dev.yaml
governance:
  enabled: true
  environment: development

  policy_engine:
    strict_mode: false              # Warnings are warnings
    require_approval: false         # No approval needed

  approval_workflow:
    minimum_approvals: 1            # Relaxed for dev
    allow_self_approval: true       # Dev can self-approve
```

### Staging Environment

```yaml
# config-staging.yaml
governance:
  enabled: true
  environment: staging

  policy_engine:
    strict_mode: false
    require_approval: true          # Test approval workflow

  approval_workflow:
    minimum_approvals: 1
    allow_self_approval: false      # Enforce separation
    approval_timeout: 12h           # Shorter timeout
```

### Production Environment

```yaml
# config-production.yaml
governance:
  enabled: true
  environment: production

  policy_engine:
    strict_mode: true               # Warnings become errors
    require_approval: true          # All risky ops need approval

  approval_workflow:
    minimum_approvals: 2            # Dual control
    allow_self_approval: false      # Strict separation
    approval_timeout: 24h
    enable_notifications: true      # Email/Slack alerts

  compliance:
    framework: SOX
    retention_period: 61320h        # 7 years
    auto_generate: true
    min_compliance_score: 80.0
```

---

## Troubleshooting

### Problem: Migration Blocked

```
Error: migration blocked by policy
```

**Solution**: Check policy violations and either:
1. Fix the migration to comply with policies
2. Request exception/approval from DBA/Manager
3. Disable strict mode for legitimate cases (with approval)

### Problem: Approval Timeout

```
Error: approval request has expired
```

**Solution**:
1. Request new approval: `gomigrate approve-request <migration-id>`
2. Increase timeout in config: `approval_timeout: 48h`
3. Notify approvers to respond faster

### Problem: Self-Approval Denied

```
Error: separation of duties violation
```

**Solution**:
1. Ask different person to approve
2. This is by design for SOX/HIPAA compliance
3. Cannot be overridden without changing config

---

## Best Practices

1. **Start with Development** - Test governance in dev first
2. **Enable Strict Mode Gradually** - Start with warnings, then strict
3. **Document Exceptions** - Track policy exception requests
4. **Regular Reports** - Generate monthly compliance reports
5. **Train Users** - Ensure team understands policies
6. **Monitor Compliance Score** - Keep above 80/100
7. **Backup Before Destructive Ops** - Always have rollback plan

---

## Support

- **Documentation**: See `governance/README.md`
- **Examples**: See `governance/examples/`
- **Issues**: Report at GitHub issues
- **Questions**: Ask in team chat or #database-migrations channel

---

## Summary

✅ **3 Integration Options** - CLI, Builder API, WebUI
✅ **Easy to Use** - Single flag enables full governance
✅ **Automatic Checks** - Policies evaluated automatically
✅ **Flexible Configuration** - Per-environment settings
✅ **Complete Audit Trail** - Every operation logged
✅ **Compliance Ready** - SOX/HIPAA/GDPR/PCI-DSS/ISO reports

**Get started in 3 steps:**

1. Enable governance: `--governance` flag
2. Configure policies: Edit `config.yaml`
3. Run migrations: Governance enforced automatically!
