/**
 * @file policy_engine.go
 * @description Policy engine for enforcing organizational rules on migrations
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/policy_engine.go
 */

package governance

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/constants"
	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/gogen-framework/gogen/pkg/core/observability/logging"
)

// PolicySeverity defines the severity of policy violations
type PolicySeverity string

const (
	PolicySeverityError   PolicySeverity = "error"   // Migration must be rejected
	PolicySeverityWarning PolicySeverity = "warning" // Migration can proceed with approval
	PolicySeverityInfo    PolicySeverity = "info"    // Informational only
)

// PolicyAction defines what action to take when a policy is violated
type PolicyAction string

const (
	PolicyActionBlock   PolicyAction = "block"   // Block the migration
	PolicyActionWarn    PolicyAction = "warn"    // Warn but allow with approval
	PolicyActionRequire PolicyAction = "require" // Require approval workflow
	PolicyActionAllow   PolicyAction = "allow"   // Allow but log
)

// PolicyViolation represents a violation of a policy
type PolicyViolation struct {
	PolicyName  string         `json:"policy_name"`
	Severity    PolicySeverity `json:"severity"`
	Message     string         `json:"message"`
	MigrationID string         `json:"migration_id"`
	Suggestion  string         `json:"suggestion,omitempty"`
	Timestamp   time.Time      `json:"timestamp"`
}

// PolicyResult represents the result of policy evaluation
type PolicyResult struct {
	Passed     bool              `json:"passed"`
	Violations []PolicyViolation `json:"violations"`
	Action     PolicyAction      `json:"action"`
	RequiresApproval bool        `json:"requires_approval"`
}

// Policy defines a single policy rule
type Policy interface {
	// GetName returns the policy name
	GetName() string

	// GetDescription returns the policy description
	GetDescription() string

	// GetSeverity returns the policy severity
	GetSeverity() PolicySeverity

	// GetAction returns the action to take on violation
	GetAction() PolicyAction

	// Evaluate evaluates the policy against a migration
	Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error)

	// IsEnabled returns whether the policy is enabled
	IsEnabled() bool

	// SetEnabled sets the enabled state
	SetEnabled(bool)
}

// BasePolicy provides common functionality for policies
type BasePolicy struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Severity    PolicySeverity `json:"severity"`
	Action      PolicyAction   `json:"action"`
	Enabled     bool           `json:"enabled"`
}

// GetName returns the policy name
func (p *BasePolicy) GetName() string {
	return p.Name
}

// GetDescription returns the policy description
func (p *BasePolicy) GetDescription() string {
	return p.Description
}

// GetSeverity returns the policy severity
func (p *BasePolicy) GetSeverity() PolicySeverity {
	return p.Severity
}

// GetAction returns the action to take on violation
func (p *BasePolicy) GetAction() PolicyAction {
	return p.Action
}

// IsEnabled returns whether the policy is enabled
func (p *BasePolicy) IsEnabled() bool {
	return p.Enabled
}

// SetEnabled sets the enabled state
func (p *BasePolicy) SetEnabled(enabled bool) {
	p.Enabled = enabled
}

// PolicyEngine evaluates policies against migrations
type PolicyEngine struct {
	policies []Policy
	config   PolicyEngineConfig
	logger   logging.ComponentLogger
}

// PolicyEngineConfig configures the policy engine
type PolicyEngineConfig struct {
	Environment         string            `json:"environment"`
	StrictMode          bool              `json:"strict_mode"`
	RequireApproval     bool              `json:"require_approval"`
	AllowedEnvironments []string          `json:"allowed_environments"`
	CustomRules         map[string]string `json:"custom_rules"`
}

// NewPolicyEngine creates a new policy engine
func NewPolicyEngine(config PolicyEngineConfig) *PolicyEngine {
	return &PolicyEngine{
		policies: make([]Policy, 0),
		config:   config,
		logger:   logging.GetComponentLogger("policy-engine"),
	}
}

// AddPolicy adds a policy to the engine
func (e *PolicyEngine) AddPolicy(policy Policy) {
	e.policies = append(e.policies, policy)
}

// RemovePolicy removes a policy by name
func (e *PolicyEngine) RemovePolicy(name string) {
	for i, p := range e.policies {
		if p.GetName() == name {
			e.policies = append(e.policies[:i], e.policies[i+1:]...)
			return
		}
	}
}

// GetPolicies returns all policies
func (e *PolicyEngine) GetPolicies() []Policy {
	return e.policies
}

// Evaluate evaluates all enabled policies against a migration
func (e *PolicyEngine) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyResult, error) {
	result := &PolicyResult{
		Passed:           true,
		Violations:       make([]PolicyViolation, 0),
		Action:           PolicyActionAllow,
		RequiresApproval: false,
	}

	e.logger.WithContext(ctx).Debug("Evaluating policies", "migration_id", migration.ID, "policy_count", len(e.policies))

	for _, policy := range e.policies {
		if !policy.IsEnabled() {
			continue
		}

		violation, err := policy.Evaluate(ctx, migration)
		if err != nil {
			e.logger.WithContext(ctx).Error(fmt.Sprintf("Policy evaluation failed for policy %s", policy.GetName()), err)
			return nil, fmt.Errorf("policy %s evaluation failed: %w", policy.GetName(), err)
		}

		if violation != nil {
			e.logger.WithContext(ctx).Info(fmt.Sprintf("Policy violation detected - policy: %s, migration: %s", policy.GetName(), migration.ID))
			result.Violations = append(result.Violations, *violation)
			result.Passed = false

			// Determine the most restrictive action
			if policy.GetAction() == PolicyActionBlock {
				result.Action = PolicyActionBlock
			} else if policy.GetAction() == PolicyActionRequire && result.Action != PolicyActionBlock {
				result.Action = PolicyActionRequire
				result.RequiresApproval = true
			} else if policy.GetAction() == PolicyActionWarn && result.Action == PolicyActionAllow {
				result.Action = PolicyActionWarn
			}
		}
	}

	// In strict mode, warnings become errors
	if e.config.StrictMode && len(result.Violations) > 0 {
		result.Action = PolicyActionBlock
	}

	// Global approval requirement
	if e.config.RequireApproval && !result.Passed {
		result.RequiresApproval = true
	}

	e.logger.WithContext(ctx).Info("Policy evaluation completed",
		"passed", result.Passed,
		"violations", len(result.Violations),
		"action", result.Action,
		"requires_approval", result.RequiresApproval)

	return result, nil
}

// EvaluateBatch evaluates policies against multiple migrations
func (e *PolicyEngine) EvaluateBatch(ctx context.Context, migrations []*types.Migration) (map[string]*PolicyResult, error) {
	results := make(map[string]*PolicyResult)

	for _, migration := range migrations {
		result, err := e.Evaluate(ctx, migration)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate migration %s: %w", migration.ID, err)
		}
		results[migration.ID] = result
	}

	return results, nil
}

// RegisterDefaultPolicies registers default enterprise policies
func (e *PolicyEngine) RegisterDefaultPolicies(environment string) {
	// No DROP TABLE in production
	e.AddPolicy(NewNoDropTableInProductionPolicy(environment))

	// Require approval for destructive operations
	e.AddPolicy(NewDestructiveOperationPolicy())

	// Require rollback SQL for destructive operations
	e.AddPolicy(NewRequireRollbackPolicy())

	// No hardcoded credentials
	e.AddPolicy(NewNoHardcodedCredentialsPolicy())

	// Require description and author
	e.AddPolicy(NewRequireMetadataPolicy())

	// Limit operation complexity
	e.AddPolicy(NewComplexityLimitPolicy())
}

// NoDropTableInProductionPolicy prevents DROP TABLE in production
type NoDropTableInProductionPolicy struct {
	BasePolicy
	environment string
}

// NewNoDropTableInProductionPolicy creates a new policy
func NewNoDropTableInProductionPolicy(environment string) *NoDropTableInProductionPolicy {
	return &NoDropTableInProductionPolicy{
		BasePolicy: BasePolicy{
			Name:        "no_drop_table_production",
			Description: "Prevents DROP TABLE statements in production environment",
			Severity:    PolicySeverityError,
			Action:      PolicyActionBlock,
			Enabled:     true,
		},
		environment: environment,
	}
}

// Evaluate evaluates the policy
func (p *NoDropTableInProductionPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	// Only enforce in production
	if !strings.EqualFold(p.environment, constants.EnvironmentProduction) {
		return nil, nil
	}

	script := strings.ToUpper(migration.Script)
	if strings.Contains(script, "DROP TABLE") || strings.Contains(script, "DROP SCHEMA") {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     "DROP TABLE/SCHEMA operations are not allowed in production",
			MigrationID: migration.ID,
			Suggestion:  "Use a different approach like marking records as deleted or contact DBA team",
			Timestamp:   time.Now(),
		}, nil
	}

	return nil, nil
}

// DestructiveOperationPolicy requires approval for destructive operations
type DestructiveOperationPolicy struct {
	BasePolicy
}

// NewDestructiveOperationPolicy creates a new policy
func NewDestructiveOperationPolicy() *DestructiveOperationPolicy {
	return &DestructiveOperationPolicy{
		BasePolicy: BasePolicy{
			Name:        "destructive_operation_approval",
			Description: "Requires approval for destructive operations (DROP, DELETE, TRUNCATE, ALTER)",
			Severity:    PolicySeverityWarning,
			Action:      PolicyActionRequire,
			Enabled:     true,
		},
	}
}

// Evaluate evaluates the policy
func (p *DestructiveOperationPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	script := strings.ToUpper(migration.Script)

	for _, op := range constants.DestructiveOperations {
		if strings.Contains(script, op) {
			return &PolicyViolation{
				PolicyName:  p.Name,
				Severity:    p.Severity,
				Message:     fmt.Sprintf("Destructive operation '%s' detected - requires approval", op),
				MigrationID: migration.ID,
				Suggestion:  "Ensure you have proper approval and backup before proceeding",
				Timestamp:   time.Now(),
			}, nil
		}
	}

	return nil, nil
}

// RequireRollbackPolicy requires rollback SQL for destructive operations
type RequireRollbackPolicy struct {
	BasePolicy
}

// NewRequireRollbackPolicy creates a new policy
func NewRequireRollbackPolicy() *RequireRollbackPolicy {
	return &RequireRollbackPolicy{
		BasePolicy: BasePolicy{
			Name:        "require_rollback_for_destructive",
			Description: "Requires rollback SQL for destructive operations",
			Severity:    PolicySeverityError,
			Action:      PolicyActionBlock,
			Enabled:     true,
		},
	}
}

// Evaluate evaluates the policy
func (p *RequireRollbackPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	script := strings.ToUpper(migration.Script)

	hasDestructive := false
	for _, op := range constants.DestructiveOperations {
		if strings.Contains(script, op) {
			hasDestructive = true
			break
		}
	}

	if hasDestructive && (migration.RollbackSQL == "" || !migration.CanRollback) {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     "Destructive operation detected but no rollback SQL provided",
			MigrationID: migration.ID,
			Suggestion:  "Add rollback SQL to allow reverting this migration if needed",
			Timestamp:   time.Now(),
		}, nil
	}

	return nil, nil
}

// NoHardcodedCredentialsPolicy prevents hardcoded credentials
type NoHardcodedCredentialsPolicy struct {
	BasePolicy
	patterns []*regexp.Regexp
}

// NewNoHardcodedCredentialsPolicy creates a new policy
func NewNoHardcodedCredentialsPolicy() *NoHardcodedCredentialsPolicy {
	patterns := make([]*regexp.Regexp, 0)
	patterns = append(patterns, regexp.MustCompile(constants.PasswordPattern))
	patterns = append(patterns, regexp.MustCompile(`(?i)(secret|token|api_key)\s*=\s*['"][^'"]+['"]`))

	return &NoHardcodedCredentialsPolicy{
		BasePolicy: BasePolicy{
			Name:        "no_hardcoded_credentials",
			Description: "Prevents hardcoded passwords, secrets, and API keys in migrations",
			Severity:    PolicySeverityError,
			Action:      PolicyActionBlock,
			Enabled:     true,
		},
		patterns: patterns,
	}
}

// Evaluate evaluates the policy
func (p *NoHardcodedCredentialsPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	for _, pattern := range p.patterns {
		if pattern.MatchString(migration.Script) {
			return &PolicyViolation{
				PolicyName:  p.Name,
				Severity:    p.Severity,
				Message:     "Hardcoded credentials detected in migration script",
				MigrationID: migration.ID,
				Suggestion:  "Use environment variables or secure vault for credentials",
				Timestamp:   time.Now(),
			}, nil
		}
	}

	return nil, nil
}

// RequireMetadataPolicy requires description and author
type RequireMetadataPolicy struct {
	BasePolicy
}

// NewRequireMetadataPolicy creates a new policy
func NewRequireMetadataPolicy() *RequireMetadataPolicy {
	return &RequireMetadataPolicy{
		BasePolicy: BasePolicy{
			Name:        "require_metadata",
			Description: "Requires description and author for all migrations",
			Severity:    PolicySeverityWarning,
			Action:      PolicyActionWarn,
			Enabled:     true,
		},
	}
}

// Evaluate evaluates the policy
func (p *RequireMetadataPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	if migration.Description == "" {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     "Migration missing description",
			MigrationID: migration.ID,
			Suggestion:  "Add a meaningful description to explain the purpose of this migration",
			Timestamp:   time.Now(),
		}, nil
	}

	if migration.Author == "" {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     "Migration missing author",
			MigrationID: migration.ID,
			Suggestion:  "Add author information to track who created this migration",
			Timestamp:   time.Now(),
		}, nil
	}

	return nil, nil
}

// ComplexityLimitPolicy limits migration complexity
type ComplexityLimitPolicy struct {
	BasePolicy
	maxStatements int
	maxLength     int
}

// NewComplexityLimitPolicy creates a new policy
func NewComplexityLimitPolicy() *ComplexityLimitPolicy {
	return &ComplexityLimitPolicy{
		BasePolicy: BasePolicy{
			Name:        "complexity_limit",
			Description: "Limits the complexity of migrations",
			Severity:    PolicySeverityWarning,
			Action:      PolicyActionWarn,
			Enabled:     true,
		},
		maxStatements: 50,
		maxLength:     100000, // 100KB
	}
}

// Evaluate evaluates the policy
func (p *ComplexityLimitPolicy) Evaluate(ctx context.Context, migration *types.Migration) (*PolicyViolation, error) {
	// Check script length
	if len(migration.Script) > p.maxLength {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     fmt.Sprintf("Migration script too large (%d bytes, max: %d)", len(migration.Script), p.maxLength),
			MigrationID: migration.ID,
			Suggestion:  "Consider splitting into multiple smaller migrations",
			Timestamp:   time.Now(),
		}, nil
	}

	// Count SQL statements (rough estimate)
	statements := strings.Count(migration.Script, ";")
	if statements > p.maxStatements {
		return &PolicyViolation{
			PolicyName:  p.Name,
			Severity:    p.Severity,
			Message:     fmt.Sprintf("Migration contains too many statements (%d, max: %d)", statements, p.maxStatements),
			MigrationID: migration.ID,
			Suggestion:  "Consider splitting into multiple smaller migrations for better rollback granularity",
			Timestamp:   time.Now(),
		}, nil
	}

	return nil, nil
}
