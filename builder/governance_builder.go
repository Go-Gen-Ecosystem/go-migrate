/**
 * @file governance_builder.go
 * @description Governance-wrapped migration tool builder for programmatic usage
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/builder/governance_builder.go
 */

package builder

import (
	"fmt"
	"time"

	migrations "github.com/gogen-framework/gogen/pkg/core/database/migrations"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/governance"
)

// GovernanceToolBuilder builds a standalone migration tool with governance
type GovernanceToolBuilder struct {
	configPath          string
	enableGovernance    bool
	environment         string
	strictMode          bool
	requireApproval     bool
	minApprovals        int
	complianceFramework string
	currentUser         string
	currentUserName     string
}

// NewGovernanceToolBuilder creates a new governance tool builder
func NewGovernanceToolBuilder() *GovernanceToolBuilder {
	return &GovernanceToolBuilder{
		environment:         "development",
		minApprovals:        2,
		complianceFramework: "SOX",
	}
}

// WithConfigPath sets the configuration file path
func (b *GovernanceToolBuilder) WithConfigPath(path string) *GovernanceToolBuilder {
	b.configPath = path
	return b
}

// WithGovernance enables governance with default settings
func (b *GovernanceToolBuilder) WithGovernance() *GovernanceToolBuilder {
	b.enableGovernance = true
	return b
}

// WithEnvironment sets the environment (development, staging, production)
func (b *GovernanceToolBuilder) WithEnvironment(env string) *GovernanceToolBuilder {
	b.environment = env
	return b
}

// WithStrictMode enables strict mode (warnings become errors)
func (b *GovernanceToolBuilder) WithStrictMode() *GovernanceToolBuilder {
	b.strictMode = true
	return b
}

// WithApprovalWorkflow enables approval workflow
func (b *GovernanceToolBuilder) WithApprovalWorkflow(minApprovals int) *GovernanceToolBuilder {
	b.requireApproval = true
	b.minApprovals = minApprovals
	return b
}

// WithComplianceFramework sets the compliance framework (SOX, HIPAA, GDPR, PCI-DSS, ISO)
func (b *GovernanceToolBuilder) WithComplianceFramework(framework string) *GovernanceToolBuilder {
	b.complianceFramework = framework
	return b
}

// WithUser sets the current user information
func (b *GovernanceToolBuilder) WithUser(userID, userName string) *GovernanceToolBuilder{
	b.currentUser = userID
	b.currentUserName = userName
	return b
}

// Build creates the migration tool, optionally wrapped with governance
func (b *GovernanceToolBuilder) Build() (migrations.StandaloneMigrationTool, error) {
	// Create base tool
	configPath := b.configPath
	if configPath == "" {
		configPath = ".gomigrate.yaml"
	}

	tool, err := migrations.NewStandaloneMigrationToolFromPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create migration tool: %w", err)
	}

	// Wrap with governance if enabled
	if b.enableGovernance {
		return b.wrapWithGovernance(tool)
	}

	return tool, nil
}

// wrapWithGovernance wraps the tool with governance
func (b *GovernanceToolBuilder) wrapWithGovernance(tool migrations.StandaloneMigrationTool) (migrations.StandaloneMigrationTool, error) {
	// Initialize governance manager
	govConfig := governance.GovernanceConfig{
		Environment: b.environment,
		PolicyEngineConfig: governance.PolicyEngineConfig{
			Environment:     b.environment,
			StrictMode:      b.strictMode,
			RequireApproval: b.requireApproval,
		},
		ApprovalConfig: governance.ApprovalWorkflowConfig{
			RequireApproval:     b.requireApproval,
			MinimumApprovals:    b.minApprovals,
			AllowSelfApproval:   false,
			ApprovalTimeout:     24 * time.Hour,
			EnableNotifications: false,
		},
		ComplianceConfig: governance.ComplianceConfig{
			Framework:       governance.ComplianceFramework(b.complianceFramework),
			OutputDir:       "./compliance-reports",
			RetentionPeriod: 7 * 365 * 24 * time.Hour,
			AutoGenerate:    false,
		},
		Enabled: true,
	}

	manager, err := governance.NewGovernanceManager(govConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize governance: %w", err)
	}

	// Use current user or default
	userID := b.currentUser
	userName := b.currentUserName
	if userID == "" {
		userID = "builder-user"
		userName = "Builder User"
	}

	// Wrap tool with governance
	return governance.NewGovernanceStandaloneTool(tool, manager, userID, userName), nil
}

// Example usage:
//
//	tool, err := builder.NewGovernanceToolBuilder().
//		WithConfigPath(".gomigrate.yaml").
//		WithGovernance().
//		WithEnvironment("production").
//		WithStrictMode().
//		WithApprovalWorkflow(2).
//		WithComplianceFramework("SOX").
//		WithUser("john.doe", "John Doe").
//		Build()
