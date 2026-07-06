/**
 * @file constraint_resolver.go
 * @description Advanced constraint resolver for migration dependencies with complex validation rules
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/resolver/constraint_resolver.go
 */

package resolver

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// ConstraintType represents different types of constraints
type ConstraintType string

const (
	ConstraintTypeVersion    ConstraintType = "version"
	ConstraintTypeTime       ConstraintType = "time"
	ConstraintTypeEnvironment ConstraintType = "environment"
	ConstraintTypeDatabase   ConstraintType = "database"
	ConstraintTypeCustom     ConstraintType = "custom"
	ConstraintTypeConflict   ConstraintType = "conflict"
	ConstraintTypeResource   ConstraintType = "resource"
)

// Constraint represents a migration constraint
type Constraint struct {
	Type        ConstraintType `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Rule        string         `json:"rule"`
	Parameters  map[string]interface{} `json:"parameters"`
	Severity    ConstraintSeverity     `json:"severity"`
	ErrorMsg    string         `json:"error_message"`
}

// ConstraintSeverity represents the severity of constraint violations
type ConstraintSeverity string

const (
	SeverityError   ConstraintSeverity = "error"
	SeverityWarning ConstraintSeverity = "warning"
	SeverityInfo    ConstraintSeverity = "info"
)

// ConstraintViolation represents a constraint violation
type ConstraintViolation struct {
	Constraint   *Constraint `json:"constraint"`
	Migration    *types.Migration `json:"migration"`
	Message      string      `json:"message"`
	Severity     ConstraintSeverity `json:"severity"`
	Suggestions  []string    `json:"suggestions"`
}

// ConstraintResolver handles advanced constraint validation
type ConstraintResolver interface {
	ValidateConstraints(ctx context.Context, migrations []*types.Migration) ([]*ConstraintViolation, error)
	AddConstraint(constraint *Constraint) error
	RemoveConstraint(name string) error
	GetConstraints() []*Constraint
	ValidateSingleMigration(ctx context.Context, migration *types.Migration) ([]*ConstraintViolation, error)
}

// DefaultConstraintResolver implements ConstraintResolver
type DefaultConstraintResolver struct {
	constraints map[string]*Constraint
	config      *ConstraintResolverConfig
	validators  map[ConstraintType]ConstraintValidator
}

// ConstraintResolverConfig configures the constraint resolver
type ConstraintResolverConfig struct {
	StrictMode          bool                       `json:"strict_mode"`
	FailOnWarnings      bool                       `json:"fail_on_warnings"`
	MaxViolations       int                        `json:"max_violations"`
	EnabledConstraints  []ConstraintType           `json:"enabled_constraints"`
	CustomValidators    map[string]ConstraintValidator `json:"-"`
	Environment         string                     `json:"environment"`
	DatabaseType        string                     `json:"database_type"`
	DatabaseVersion     string                     `json:"database_version"`
}

// ConstraintValidator validates specific constraint types
type ConstraintValidator interface {
	Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error)
	GetType() ConstraintType
}

// NewDefaultConstraintResolver creates a new constraint resolver
func NewDefaultConstraintResolver(config *ConstraintResolverConfig) *DefaultConstraintResolver {
	if config == nil {
		config = &ConstraintResolverConfig{
			StrictMode:         false,
			FailOnWarnings:     false,
			MaxViolations:      100,
			EnabledConstraints: []ConstraintType{ConstraintTypeVersion, ConstraintTypeTime, ConstraintTypeConflict},
		}
	}

	resolver := &DefaultConstraintResolver{
		constraints: make(map[string]*Constraint),
		config:      config,
		validators:  make(map[ConstraintType]ConstraintValidator),
	}

	// Register default validators
	resolver.registerDefaultValidators()

	return resolver
}

// registerDefaultValidators registers built-in constraint validators
func (r *DefaultConstraintResolver) registerDefaultValidators() {
	validators := []ConstraintValidator{
		&VersionConstraintValidator{},
		&TimeConstraintValidator{},
		&EnvironmentConstraintValidator{},
		&DatabaseConstraintValidator{},
		&ConflictConstraintValidator{},
		&ResourceConstraintValidator{},
	}

	for _, validator := range validators {
		r.validators[validator.GetType()] = validator
	}
}

// ValidateConstraints validates all constraints against migrations
func (r *DefaultConstraintResolver) ValidateConstraints(ctx context.Context, migrations []*types.Migration) ([]*ConstraintViolation, error) {
	violations := make([]*ConstraintViolation, 0)

	for _, migration := range migrations {
		migrationViolations, err := r.ValidateSingleMigration(ctx, migration)
		if err != nil {
			return nil, fmt.Errorf("failed to validate migration %s: %w", migration.ID, err)
		}
		violations = append(violations, migrationViolations...)

		if len(violations) >= r.config.MaxViolations {
			break
		}
	}

	return violations, nil
}

// ValidateSingleMigration validates constraints for a single migration
func (r *DefaultConstraintResolver) ValidateSingleMigration(ctx context.Context, migration *types.Migration) ([]*ConstraintViolation, error) {
	violations := make([]*ConstraintViolation, 0)

	for _, constraint := range r.constraints {
		// Check if constraint type is enabled
		if !r.isConstraintTypeEnabled(constraint.Type) {
			continue
		}

		validator, exists := r.validators[constraint.Type]
		if !exists {
			continue
		}

		violation, err := validator.Validate(ctx, constraint, migration, r.config)
		if err != nil {
			return nil, fmt.Errorf("constraint validation failed for %s: %w", constraint.Name, err)
		}

		if violation != nil {
			violations = append(violations, violation)
		}
	}

	return violations, nil
}

// AddConstraint adds a new constraint
func (r *DefaultConstraintResolver) AddConstraint(constraint *Constraint) error {
	if constraint.Name == "" {
		return fmt.Errorf("constraint name cannot be empty")
	}

	r.constraints[constraint.Name] = constraint
	return nil
}

// RemoveConstraint removes a constraint
func (r *DefaultConstraintResolver) RemoveConstraint(name string) error {
	if _, exists := r.constraints[name]; !exists {
		return fmt.Errorf("constraint %s not found", name)
	}

	delete(r.constraints, name)
	return nil
}

// GetConstraints returns all constraints
func (r *DefaultConstraintResolver) GetConstraints() []*Constraint {
	constraints := make([]*Constraint, 0, len(r.constraints))
	for _, constraint := range r.constraints {
		constraints = append(constraints, constraint)
	}
	return constraints
}

// isConstraintTypeEnabled checks if a constraint type is enabled
func (r *DefaultConstraintResolver) isConstraintTypeEnabled(constraintType ConstraintType) bool {
	for _, enabled := range r.config.EnabledConstraints {
		if enabled == constraintType {
			return true
		}
	}
	return false
}

// VersionConstraintValidator validates version constraints
type VersionConstraintValidator struct{}

func (v *VersionConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeVersion
}

func (v *VersionConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate version format and ordering
	versionPattern := regexp.MustCompile(`^v?\d+\.\d+\.\d+`)
	if !versionPattern.MatchString(migration.Version) {
		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    fmt.Sprintf("Invalid version format: %s", migration.Version),
			Severity:   SeverityError,
			Suggestions: []string{
				"Use semantic versioning format (e.g., v1.0.0)",
				"Ensure version follows pattern: major.minor.patch",
			},
		}, nil
	}

	return nil, nil
}

// TimeConstraintValidator validates time-based constraints
type TimeConstraintValidator struct{}

func (v *TimeConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeTime
}

func (v *TimeConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate migration timing constraints
	if migration.Timestamp.IsZero() {
		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    "Migration timestamp is required",
			Severity:   SeverityError,
			Suggestions: []string{
				"Add timestamp to migration metadata",
				"Use current time if timestamp is unknown",
			},
		}, nil
	}

	// Check if migration is too old or too new
	now := time.Now()
	if migration.Timestamp.After(now.Add(24 * time.Hour)) {
		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    "Migration timestamp is in the future",
			Severity:   SeverityWarning,
			Suggestions: []string{
				"Verify migration timestamp is correct",
				"Use current time for new migrations",
			},
		}, nil
	}

	return nil, nil
}

// EnvironmentConstraintValidator validates environment constraints
type EnvironmentConstraintValidator struct{}

func (v *EnvironmentConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeEnvironment
}

func (v *EnvironmentConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate environment-specific constraints
	allowedEnvs, ok := constraint.Parameters["allowed_environments"].([]string)
	if ok && config.Environment != "" {
		for _, env := range allowedEnvs {
			if env == config.Environment {
				return nil, nil
			}
		}

		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    fmt.Sprintf("Migration not allowed in environment: %s", config.Environment),
			Severity:   SeverityError,
			Suggestions: []string{
				fmt.Sprintf("Run migration in allowed environments: %v", allowedEnvs),
				"Update migration environment constraints",
			},
		}, nil
	}

	return nil, nil
}

// DatabaseConstraintValidator validates database-specific constraints
type DatabaseConstraintValidator struct{}

func (v *DatabaseConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeDatabase
}

func (v *DatabaseConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate database type compatibility
	supportedDbs, ok := constraint.Parameters["supported_databases"].([]string)
	if ok && config.DatabaseType != "" {
		for _, db := range supportedDbs {
			if strings.EqualFold(db, config.DatabaseType) {
				return nil, nil
			}
		}

		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    fmt.Sprintf("Migration not compatible with database: %s", config.DatabaseType),
			Severity:   SeverityError,
			Suggestions: []string{
				fmt.Sprintf("Use supported databases: %v", supportedDbs),
				"Create database-specific migration variants",
			},
		}, nil
	}

	return nil, nil
}

// ConflictConstraintValidator validates conflict constraints
type ConflictConstraintValidator struct{}

func (v *ConflictConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeConflict
}

func (v *ConflictConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate for potential conflicts
	conflictPatterns, ok := constraint.Parameters["conflict_patterns"].([]string)
	if ok {
		for _, pattern := range conflictPatterns {
			if matched, _ := regexp.MatchString(pattern, migration.SQL); matched {
				return &ConstraintViolation{
					Constraint: constraint,
					Migration:  migration,
					Message:    fmt.Sprintf("Migration contains potentially conflicting SQL pattern: %s", pattern),
					Severity:   SeverityWarning,
					Suggestions: []string{
						"Review SQL for potential conflicts",
						"Consider breaking migration into smaller parts",
						"Add explicit dependency declarations",
					},
				}, nil
			}
		}
	}

	return nil, nil
}

// ResourceConstraintValidator validates resource constraints
type ResourceConstraintValidator struct{}

func (v *ResourceConstraintValidator) GetType() ConstraintType {
	return ConstraintTypeResource
}

func (v *ResourceConstraintValidator) Validate(ctx context.Context, constraint *Constraint, migration *types.Migration, config *ConstraintResolverConfig) (*ConstraintViolation, error) {
	// Validate resource usage constraints
	maxSize, ok := constraint.Parameters["max_sql_size"].(int)
	if ok && len(migration.SQL) > maxSize {
		return &ConstraintViolation{
			Constraint: constraint,
			Migration:  migration,
			Message:    fmt.Sprintf("Migration SQL size (%d) exceeds maximum allowed (%d)", len(migration.SQL), maxSize),
			Severity:   SeverityWarning,
			Suggestions: []string{
				"Break large migration into smaller parts",
				"Consider using data migration scripts",
				"Review SQL optimization opportunities",
			},
		}, nil
	}

	return nil, nil
}
