/**
 * @file validator.go
 * @description Migration validation implementations with comprehensive checks
 * @author Admilson B. F. Cossa
 * @date 2025-01-22
 * @path pkg/core/database/migrations/validator.go
 */

package validator

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/logging"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/constants"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/lock"
	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"gorm.io/gorm"
)

// CompositeMigrationValidator implements MigrationValidator with multiple validation strategies
type CompositeMigrationValidator struct {
	validators []types.MigrationValidator
	db         *gorm.DB
	config     *types.Config
	logger     logging.ComponentLogger
}

// SyntaxMigrationValidator validates SQL syntax
type SyntaxMigrationValidator struct {
	db *gorm.DB
}

// SecurityMigrationValidator validates security aspects of migrations
type SecurityMigrationValidator struct {
	config *types.Config
	logger logging.ComponentLogger
}

// DependencyMigrationValidator validates migration dependencies
type DependencyMigrationValidator struct {
	tracker types.MigrationTracker
	migrationMap map[string]*types.Migration // Cache for loaded migrations
}

// PerformanceMigrationValidator validates performance implications
type PerformanceMigrationValidator struct {
	db     *gorm.DB
	config *types.Config
	strict bool // When false, converts errors to warnings for non-critical issues
}

// BusinessRuleMigrationValidator validates business rules
type BusinessRuleMigrationValidator struct {
	rules map[string]ValidationRule
}

// ValidationRule represents a business rule for validation
type ValidationRule struct {
	Name        string
	Description string
	Validator   func(*types.Migration) error
	Severity    ValidationSeverity
}

// MigrationValidatorAdapter adapts types.MigrationValidator to the main MigrationValidator interface
type MigrationValidatorAdapter struct {
	validator types.MigrationValidator
}

// NewMigrationValidatorAdapter creates a new adapter
func NewMigrationValidatorAdapter(validator types.MigrationValidator) *MigrationValidatorAdapter {
	return &MigrationValidatorAdapter{validator: validator}
}

// ValidateMigration validates a migration with context (main interface method)
func (a *MigrationValidatorAdapter) ValidateMigration(ctx context.Context, migration *types.Migration) error {
	return a.validator.ValidateMigration(migration)
}

// ValidateMigrations validates multiple migrations with context (main interface method)
func (a *MigrationValidatorAdapter) ValidateMigrations(ctx context.Context, migrations []*types.Migration) error {
	// Convert to slice of values for the engine interface
	migrationValues := make([]types.Migration, len(migrations))
	for i, migration := range migrations {
		migrationValues[i] = *migration
	}
	return a.validator.ValidateMigrations(migrationValues)
}

// ValidateChecksum validates a migration checksum (main interface method)
func (a *MigrationValidatorAdapter) ValidateChecksum(migration *types.Migration) error {
	// For basic engine validators, checksum validation is not implemented
	// This would need to be implemented by specific validators that support it
	return nil
}

// ValidateDependencies validates migration dependencies (main interface method)
func (a *MigrationValidatorAdapter) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// Convert to slice of values for the engine interface
	migrationValues := make([]types.Migration, len(migrations))
	for i, migration := range migrations {
		migrationValues[i] = *migration
	}
	return a.validator.CheckDependencies(migrationValues)
}

// ValidationSeverity represents the severity of a validation issue
// ValidationSeverity and environment constants are imported from constants package
type ValidationSeverity = constants.ValidationSeverity

const (
	SeverityError   = constants.SeverityError
	SeverityWarning = constants.SeverityWarning
	SeverityInfo    = constants.SeverityInfo
)

const (
	EnvironmentProduction  = constants.EnvironmentProduction
	EnvironmentStaging     = constants.EnvironmentStaging
	EnvironmentDevelopment = constants.EnvironmentDevelopment
)

// Using constants from constants package to avoid duplication
var (
	DestructiveOperations = constants.DestructiveOperations
	SQLStatementKeywords  = constants.SQLStatementKeywords
	SQLQueryKeywords      = constants.SQLQueryKeywords
)

// ValidationResult represents the result of a validation
type ValidationResult struct {
	Valid    bool
	Issues   []ValidationIssue
	Warnings []ValidationIssue
	Info     []ValidationIssue
}

// ValidationIssue represents a validation issue
type ValidationIssue struct {
	Severity   ValidationSeverity
	Code       string
	Message    string
	Line       int
	Column     int
	Suggestion string
	RuleName   string
	Context    map[string]interface{}
}

// NewCompositeMigrationValidator creates a new composite migration validator
func NewCompositeMigrationValidator(db *gorm.DB, config *types.Config, tracker types.MigrationTracker) *CompositeMigrationValidator {
	validator := &CompositeMigrationValidator{
		validators: make([]types.MigrationValidator, 0),
		db:         db,
		config:     config,
		logger:     logging.GetComponentLogger("migration-validator-composite"),
	}

	// Add default validators
	validator.AddValidator(NewSyntaxMigrationValidator(db))
	validator.AddValidator(NewSecurityMigrationValidator(config))
	validator.AddValidator(NewDependencyMigrationValidator(tracker))
	validator.AddValidator(NewPerformanceMigrationValidator(db, config))
	validator.AddValidator(NewBusinessRuleMigrationValidator())

	return validator
}

// AddValidator adds a validator to the composite validator
func (v *CompositeMigrationValidator) AddValidator(validator types.MigrationValidator) {
	v.validators = append(v.validators, validator)
}

// ValidateMigration validates a migration using all registered validators (engine interface method without context)
func (v *CompositeMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationWithContext validates a migration using all registered validators (main interface method with context)
func (v *CompositeMigrationValidator) ValidateMigrationWithContext(ctx context.Context, migration *types.Migration) error {
	return v.ValidateMigrationCtx(ctx, migration)
}

// ValidateMigrationCtx validates a migration using all registered validators with context
func (v *CompositeMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	var allErrors []string

	for _, validator := range v.validators {
		if err := validator.ValidateMigration(migration); err != nil {
			allErrors = append(allErrors, err.Error())
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("migration validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// calculateChecksum calculates MD5 checksum of the given content
func (v *CompositeMigrationValidator) calculateChecksum(content string) string {
	hash := md5.Sum([]byte(content))
	return hex.EncodeToString(hash[:])
}

// ValidateMigrationDetailed performs detailed validation with structured results
func (v *CompositeMigrationValidator) ValidateMigrationDetailed(ctx context.Context, migration *types.Migration) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Issues:   make([]ValidationIssue, 0),
		Warnings: make([]ValidationIssue, 0),
		Info:     make([]ValidationIssue, 0),
	}

	// Run all validators
	for _, validator := range v.validators {
		if detailedValidator, ok := validator.(DetailedMigrationValidator); ok {
			validatorResult := detailedValidator.ValidateMigrationDetailed(ctx, migration)
			result.Issues = append(result.Issues, validatorResult.Issues...)
			result.Warnings = append(result.Warnings, validatorResult.Warnings...)
			result.Info = append(result.Info, validatorResult.Info...)
		} else {
			// Fallback to basic validation
			if err := validator.ValidateMigration(migration); err != nil {
				issue := ValidationIssue{
					Severity: SeverityError,
					Code:     "VALIDATION_ERROR",
					Message:  err.Error(),
					RuleName: "basic_validation",
				}
				result.Issues = append(result.Issues, issue)
			}
		}
	}

	// Set overall validity
	result.Valid = len(result.Issues) == 0

	return result
}

// DetailedMigrationValidator interface for detailed validation
type DetailedMigrationValidator interface {
	types.MigrationValidator
	ValidateMigrationDetailed(ctx context.Context, migration *types.Migration) *ValidationResult
}

// Syntax Validator Implementation

// NewSyntaxMigrationValidator creates a new syntax migration validator
func NewSyntaxMigrationValidator(db *gorm.DB) *SyntaxMigrationValidator {
	return &SyntaxMigrationValidator{db: db}
}

// ValidateMigration validates SQL syntax (interface method)
func (v *SyntaxMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationCtx validates SQL syntax with context
func (v *SyntaxMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	// Basic syntax validation
	if err := v.validateBasicSyntax(migration.Script); err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}

	// Database-specific syntax validation
	if v.db != nil {
		if err := v.validateDatabaseSyntax(ctx, migration.Script); err != nil {
			return fmt.Errorf("database syntax error: %w", err)
		}
	}

	return nil
}

// CheckDependencies checks dependencies for syntax validators (no-op)
func (v *SyntaxMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	return nil
}

// CheckCircularDependencies checks for circular dependencies (no-op for syntax validator)
func (v *SyntaxMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	return nil
}

// ValidateMigrationDetailed performs detailed syntax validation
func (v *SyntaxMigrationValidator) ValidateMigrationDetailed(ctx context.Context, migration *types.Migration) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Issues:   make([]ValidationIssue, 0),
		Warnings: make([]ValidationIssue, 0),
		Info:     make([]ValidationIssue, 0),
	}

	// Check for common syntax issues
	issues := v.checkSyntaxIssues(migration.Script)
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			result.Issues = append(result.Issues, issue)
		case SeverityWarning:
			result.Warnings = append(result.Warnings, issue)
		case SeverityInfo:
			result.Info = append(result.Info, issue)
		}
	}

	result.Valid = len(result.Issues) == 0
	return result
}

// validateBasicSyntax performs basic SQL syntax validation
func (v *SyntaxMigrationValidator) validateBasicSyntax(content string) error {
	// Check for balanced parentheses
	if err := v.checkBalancedParentheses(content); err != nil {
		return err
	}

	// Check for balanced quotes
	if err := v.checkBalancedQuotes(content); err != nil {
		return err
	}

	// Check for valid SQL keywords
	if err := v.checkSQLKeywords(content); err != nil {
		return err
	}

	return nil
}

// validateDatabaseSyntax validates syntax against the actual database
func (v *SyntaxMigrationValidator) validateDatabaseSyntax(ctx context.Context, content string) error {
	// Use EXPLAIN to validate syntax without executing
	statements := v.splitStatements(content)

	// Get database dialect to handle database-specific limitations
	dialect := v.db.Dialector.Name()

	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Skip certain statements that can't be explained
		if v.isNonExplainableStatement(stmt) {
			continue
		}

		// SQLite doesn't support EXPLAIN for DDL statements
		if dialect == "sqlite" && v.isDDLStatement(stmt) {
			// For SQLite DDL statements, just do basic syntax validation
			if err := v.validateSQLiteDDLSyntax(stmt); err != nil {
				return fmt.Errorf("invalid SQL statement: %s", stmt)
			}
			continue
		}

		// Try to explain the statement
		explainSQL := fmt.Sprintf("EXPLAIN %s", stmt)
		
		// Execute the EXPLAIN and capture any results
		var result []map[string]interface{}
		tx := v.db.WithContext(ctx).Raw(explainSQL).Scan(&result)
		if tx.Error != nil {
			return fmt.Errorf("invalid SQL statement: %s", stmt)
		}
	}

	return nil
}

// checkSyntaxIssues checks for various syntax issues
func (v *SyntaxMigrationValidator) checkSyntaxIssues(content string) []ValidationIssue {
	var issues []ValidationIssue

	lines := strings.Split(content, "\n")
	for lineNum, line := range lines {
		// Check for common issues
		if strings.Contains(strings.ToUpper(line), "SELECT *") {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityWarning,
				Code:       "SELECT_STAR",
				Message:    "Avoid using SELECT * in migrations",
				Line:       lineNum + 1,
				Suggestion: "Specify column names explicitly",
				RuleName:   "syntax_best_practices",
			})
		}

		// Check for missing semicolons
		trimmed := strings.TrimSpace(line)
		if v.isStatementLine(trimmed) && !strings.HasSuffix(trimmed, ";") {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityWarning,
				Code:       "MISSING_SEMICOLON",
				Message:    "Statement may be missing semicolon",
				Line:       lineNum + 1,
				Suggestion: "Add semicolon at the end of the statement",
				RuleName:   "syntax_formatting",
			})
		}
	}

	return issues
}

// Security Validator Implementation

// NewSecurityMigrationValidator creates a new security migration validator
func NewSecurityMigrationValidator(config *types.Config) *SecurityMigrationValidator {
	return &SecurityMigrationValidator{
		config: config,
		logger: logging.GetComponentLogger("migration-validator-security"),
	}
}

// ValidateMigration validates security aspects (interface method)
func (v *SecurityMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationCtx validates security aspects with context
func (v *SecurityMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	// Check for dangerous operations
	if err := v.checkDangerousOperations(migration.Script); err != nil {
		return fmt.Errorf("security violation: %w", err)
	}

	// Check for SQL injection patterns
	if err := v.checkSQLInjectionPatterns(migration.Script); err != nil {
		return fmt.Errorf("potential SQL injection: %w", err)
	}

	// Check for privilege escalation
	if err := v.checkPrivilegeEscalation(migration.Script); err != nil {
		return fmt.Errorf("privilege escalation detected: %w", err)
	}

	return nil
}

// ValidateMigrations validates multiple migrations for security issues
func (v *SecurityMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckDependencies checks dependencies for security validators (no-op)
func (v *SecurityMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	return nil
}

// CheckCircularDependencies checks for circular dependencies (no-op for security validator)
func (v *SecurityMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	return nil
}

// ValidateMigrationDetailed performs detailed security validation
func (v *SecurityMigrationValidator) ValidateMigrationDetailed(ctx context.Context, migration *types.Migration) *ValidationResult {
	result := &ValidationResult{
		Valid:    true,
		Issues:   make([]ValidationIssue, 0),
		Warnings: make([]ValidationIssue, 0),
		Info:     make([]ValidationIssue, 0),
	}

	// Check security issues
	issues := v.checkSecurityIssues(migration.Script)
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			result.Issues = append(result.Issues, issue)
		case SeverityWarning:
			result.Warnings = append(result.Warnings, issue)
		case SeverityInfo:
			result.Info = append(result.Info, issue)
		}
	}

	result.Valid = len(result.Issues) == 0
	return result
}

// checkDangerousOperations checks for dangerous SQL operations
func (v *SecurityMigrationValidator) checkDangerousOperations(content string) error {
	dangerousPatterns := []string{
		`(?i)\bDROP\s+DATABASE\b`,
		`(?i)\bTRUNCATE\s+TABLE\b`,
		`(?i)\bDELETE\s+FROM\s+\w+\s*;?\s*$`,
		`(?i)\bGRANT\s+ALL\b`,
		`(?i)\bCREATE\s+USER\b`,
		`(?i)\bALTER\s+USER\b`,
		`(?i)\bDROP\s+USER\b`,
	}

	for _, pattern := range dangerousPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			if v.config.Environment == EnvironmentProduction {
				return fmt.Errorf("dangerous operation not allowed in production: %s", pattern)
			}
		}
	}

	return nil
}

// checkSQLInjectionPatterns checks for potential SQL injection patterns
func (v *SecurityMigrationValidator) checkSQLInjectionPatterns(content string) error {
	injectionPatterns := []string{
		`(?i)'\s*OR\s*'1'\s*=\s*'1`,
		`(?i)'\s*OR\s*1\s*=\s*1`,
		`(?i)'\s*UNION\s+SELECT`,
		`(?i)'\s*;\s*DROP\s+TABLE`,
		`(?i)'\s*;\s*DELETE\s+FROM`,
	}

	for _, pattern := range injectionPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return fmt.Errorf("potential SQL injection pattern detected: %s", pattern)
		}
	}

	return nil
}

// checkPrivilegeEscalation checks for privilege escalation attempts
func (v *SecurityMigrationValidator) checkPrivilegeEscalation(content string) error {
	privilegePatterns := []string{
		`(?i)\bGRANT\s+.*\s+TO\s+.*\s+WITH\s+GRANT\s+OPTION\b`,
		`(?i)\bGRANT\s+SUPER\b`,
		`(?i)\bGRANT\s+FILE\b`,
		`(?i)\bGRANT\s+PROCESS\b`,
		`(?i)\bGRANT\s+RELOAD\b`,
		`(?i)\bGRANT\s+SHUTDOWN\b`,
	}

	for _, pattern := range privilegePatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return fmt.Errorf("privilege escalation detected: %s", pattern)
		}
	}

	return nil
}

// checkSecurityIssues checks for various security issues
func (v *SecurityMigrationValidator) checkSecurityIssues(content string) []ValidationIssue {
	var issues []ValidationIssue

	// Check for hardcoded passwords
	passwordPattern := `(?i)(password|pwd)\s*=\s*['"][^'"]+['"]`
	if matched, _ := regexp.MatchString(passwordPattern, content); matched {
		issues = append(issues, ValidationIssue{
			Severity:   SeverityError,
			Code:       "HARDCODED_PASSWORD",
			Message:    "Hardcoded password detected in migration",
			Suggestion: "Use environment variables or secure configuration",
			RuleName:   "security_credentials",
		})
	}

	// Check for weak permissions
	weakPermPattern := `(?i)GRANT\s+ALL\s+PRIVILEGES`
	if matched, _ := regexp.MatchString(weakPermPattern, content); matched {
		issues = append(issues, ValidationIssue{
			Severity:   SeverityWarning,
			Code:       "WEAK_PERMISSIONS",
			Message:    "Granting ALL PRIVILEGES may be too permissive",
			Suggestion: "Grant only necessary privileges",
			RuleName:   "security_permissions",
		})
	}

	return issues
}

// ValidateMigrationsCtx validates multiple migrations for security with context
func (v *SecurityMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigrationCtx(ctx, migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("security validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema for security compliance
func (v *SecurityMigrationValidator) ValidateSchema(ctx context.Context) error {
	// For security validator, schema validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateChecksums validates migration checksums for security consistency
func (v *SecurityMigrationValidator) ValidateChecksums(ctx context.Context) error {
	// For security validator, checksum validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateDependencies validates migration dependencies for security compatibility
func (v *SecurityMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// For security validator, dependency validation is not applicable
	// This is handled by dependency validator
	return nil
}

// Dependency Validator Implementation

// NewDependencyMigrationValidator creates a new dependency validator
func NewDependencyMigrationValidator(tracker types.MigrationTracker) *DependencyMigrationValidator {
	return &DependencyMigrationValidator{
		tracker: tracker,
		migrationMap: make(map[string]*types.Migration),
	}
}

// ValidateMigration validates migration dependencies (interface method)
func (v *DependencyMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationCtx validates migration dependencies with context
func (v *DependencyMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	// Check if all dependencies are satisfied
	for _, depID := range migration.Dependencies {
		applied, err := v.tracker.IsMigrationApplied(ctx, depID)
		if err != nil {
			return fmt.Errorf("failed to check dependency %s: %w", depID, err)
		}
		if !applied {
			return fmt.Errorf("dependency %s is not applied", depID)
		}
	}

	// Check for circular dependencies
	if err := v.checkCircularDependencies(ctx, migration); err != nil {
		return fmt.Errorf("circular dependency detected: %w", err)
	}

	return nil
}

// checkCircularDependencies checks for circular dependencies
func (v *DependencyMigrationValidator) checkCircularDependencies(ctx context.Context, migration *types.Migration) error {
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	// For single migration validation, we only check if the migration has dependencies
	// that would create a circular reference. We don't need to validate the entire
	// dependency tree since that will be done during batch validation.
	return v.dfsCircularCheckSingle(ctx, migration, visited, recursionStack)
}

// checkCircularDependenciesWithMap checks for circular dependencies using migration map
func (v *DependencyMigrationValidator) checkCircularDependenciesWithMap(ctx context.Context, migration *types.Migration) error {
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)

	return v.dfsCircularCheckWithMap(ctx, migration.ID, visited, recursionStack)
}

// dfsCircularCheck performs depth-first search to detect circular dependencies
func (v *DependencyMigrationValidator) dfsCircularCheck(ctx context.Context, migrationID string, visited, recursionStack map[string]bool) error {
	visited[migrationID] = true
	recursionStack[migrationID] = true

	// Get migration dependencies
	migration, err := v.tracker.GetMigration(ctx, migrationID)
	if err != nil {
		return err
	}

	for _, depID := range migration.Dependencies {
		if !visited[depID] {
			if err := v.dfsCircularCheck(ctx, depID, visited, recursionStack); err != nil {
				return err
			}
		} else if recursionStack[depID] {
			return fmt.Errorf("circular dependency: %s -> %s", migrationID, depID)
		}
	}

	recursionStack[migrationID] = false
	return nil
}

// dfsCircularCheckSingle performs circular dependency check for a single migration
// without requiring database lookups for dependencies
func (v *DependencyMigrationValidator) dfsCircularCheckSingle(ctx context.Context, migration *types.Migration, visited, recursionStack map[string]bool) error {
	visited[migration.ID] = true
	recursionStack[migration.ID] = true

	// For single migration validation, we only check direct dependencies
	// The full dependency tree validation will be done during batch validation
	for _, depID := range migration.Dependencies {
		// Check if this dependency would create a direct circular reference
		if recursionStack[depID] {
			return fmt.Errorf("circular dependency: %s -> %s", migration.ID, depID)
		}
		
		// Check if the dependency is applied (this is safe to check)
		applied, err := v.tracker.IsMigrationApplied(ctx, depID)
		if err != nil {
			return fmt.Errorf("failed to check dependency %s: %w", depID, err)
		}
		
		// If dependency is not applied, we can't validate the full chain
		// but we can at least ensure it's not a direct circular reference
		if !applied {
			// For unapplied dependencies, we just mark them as visited
			// to prevent infinite loops in case of direct circular references
			visited[depID] = true
		}
	}

	recursionStack[migration.ID] = false
	return nil
}

// dfsCircularCheckWithMap performs depth-first search using migration map
func (v *DependencyMigrationValidator) dfsCircularCheckWithMap(ctx context.Context, migrationID string, visited, recursionStack map[string]bool) error {
	visited[migrationID] = true
	recursionStack[migrationID] = true

	// Get migration from map instead of tracker
	migration, exists := v.migrationMap[migrationID]
	if !exists {
		// If migration not in map, it might be a dependency that's not loaded
		// This is not necessarily an error for circular dependency detection
		recursionStack[migrationID] = false
		return nil
	}

	for _, depID := range migration.Dependencies {
		if !visited[depID] {
			if err := v.dfsCircularCheckWithMap(ctx, depID, visited, recursionStack); err != nil {
				return err
			}
		} else if recursionStack[depID] {
			return fmt.Errorf("circular dependency: %s -> %s", migrationID, depID)
		}
	}

	recursionStack[migrationID] = false
	return nil
}

// ValidateMigrations validates multiple migrations for dependencies
func (v *DependencyMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckDependencies checks dependencies between migrations
func (v *DependencyMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	return v.ValidateMigrations(migrations)
}

// CheckCircularDependencies checks for circular dependencies
func (v *DependencyMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	// Populate migration map for dependency resolution
	v.migrationMap = make(map[string]*types.Migration)
	for i := range migrations {
		v.migrationMap[migrations[i].ID] = &migrations[i]
	}
	
	// Build a dependency graph and check for cycles
	for _, migration := range migrations {
		if err := v.checkCircularDependenciesWithMap(context.Background(), &migration); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMigrationsCtx validates multiple migrations for dependencies with context
func (v *DependencyMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigration(migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("dependency validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema for dependency consistency
func (v *DependencyMigrationValidator) ValidateSchema(ctx context.Context) error {
	// For dependency validator, schema validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateChecksums validates migration checksums for dependency consistency
func (v *DependencyMigrationValidator) ValidateChecksums(ctx context.Context) error {
	// For dependency validator, checksum validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateDependencies validates migration dependencies - this is the main function for this validator
func (v *DependencyMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// Check all migrations for dependency issues
	for _, migration := range migrations {
		if err := v.ValidateMigration(migration); err != nil {
			return fmt.Errorf("dependency validation failed for migration %s: %w", migration.ID, err)
		}
	}
	return nil
}

// Performance Validator Implementation

// NewPerformanceMigrationValidator creates a new performance migration validator
func NewPerformanceMigrationValidator(db *gorm.DB, config *types.Config) *PerformanceMigrationValidator {
	// Use strict mode from config if available, otherwise default to false (lenient)
	strict := false
	if config != nil && config.Migration.StrictMode {
		strict = true
	}
	return &PerformanceMigrationValidator{
		db:     db,
		config: config,
		strict: strict,
	}
}

// NewStrictPerformanceMigrationValidator creates a new strict performance migration validator
func NewStrictPerformanceMigrationValidator(db *gorm.DB, config *types.Config) *PerformanceMigrationValidator {
	return &PerformanceMigrationValidator{
		db:     db,
		config: config,
		strict: true,
	}
}

// ValidateMigration validates performance implications (interface method)
func (v *PerformanceMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationCtx validates performance implications with context
func (v *PerformanceMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	// Check for potentially slow operations
	if err := v.checkSlowOperations(migration.Script); err != nil {
		return fmt.Errorf("performance issue: %w", err)
	}

	// Check for missing indexes
	if err := v.checkMissingIndexes(migration.Script); err != nil {
		return fmt.Errorf("index issue: %w", err)
	}

	// Estimate execution time
	if err := v.estimateExecutionTime(ctx, migration); err != nil {
		return fmt.Errorf("execution time estimation failed: %w", err)
	}

	return nil
}

// checkSlowOperations checks for potentially slow operations
func (v *PerformanceMigrationValidator) checkSlowOperations(content string) error {
	slowPatterns := []string{
		`(?i)\bALTER\s+TABLE\s+\w+\s+ADD\s+COLUMN\s+.*\s+NOT\s+NULL\b`,
		`(?i)\bCREATE\s+INDEX\s+.*\s+ON\s+\w+\s*\([^)]*\)\s*WHERE\b`,
		`(?i)\bUPDATE\s+\w+\s+SET\s+.*\s+WHERE\s+.*\s+IN\s*\(\s*SELECT\b`,
	}

	for _, pattern := range slowPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return fmt.Errorf("potentially slow operation detected: %s", pattern)
		}
	}

	return nil
}

// checkMissingIndexes checks for missing indexes on foreign keys
func (v *PerformanceMigrationValidator) checkMissingIndexes(content string) error {
	// Look for foreign key constraints without corresponding indexes
	fkPattern := `(?i)FOREIGN\s+KEY\s*\(\s*(\w+)\s*\)\s+REFERENCES`
	re := regexp.MustCompile(fkPattern)
	matches := re.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			column := match[1]
			// Check if there's a corresponding index
			indexPattern := fmt.Sprintf(`(?i)CREATE\s+INDEX\s+.*\s+ON\s+\w+\s*\(\s*%s\s*\)`, column)
			if matched, _ := regexp.MatchString(indexPattern, content); !matched {
				// In strict mode, this is an error
				// In lenient mode, we log a warning but don't fail
				if v.strict {
					return fmt.Errorf("foreign key column %s may need an index for performance", column)
				}
				// In lenient mode, just continue (would ideally log a warning here)
				// The warning will be captured by ValidateMigrationDetailed if available
			}
		}
	}

	return nil
}

// estimateExecutionTime estimates migration execution time
func (v *PerformanceMigrationValidator) estimateExecutionTime(ctx context.Context, migration *types.Migration) error {
	// This is a simplified estimation
	// In a real implementation, this would analyze the migration content
	// and estimate based on table sizes, operation types, etc.

	estimatedTime := time.Second * 30 // Default estimation

	// Check for operations that typically take longer
	if strings.Contains(strings.ToUpper(migration.Script), "ALTER TABLE") {
		estimatedTime = time.Minute * 5
	}
	if strings.Contains(strings.ToUpper(migration.Script), "CREATE INDEX") {
		estimatedTime = time.Minute * 2
	}

	// Warn if estimated time is too long
	if estimatedTime > time.Minute*10 && v.config.Environment == EnvironmentProduction {
		return fmt.Errorf("migration estimated to take %v, which may be too long for production", estimatedTime)
	}

	return nil
}

// ValidateMigrations validates multiple migrations for performance
func (v *PerformanceMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckDependencies checks dependencies for performance validators (no-op)
func (v *PerformanceMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	return nil
}

// CheckCircularDependencies checks for circular dependencies (no-op for performance validator)
func (v *PerformanceMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	return nil
}

// ValidateMigrationsCtx validates multiple migrations for performance with context
func (v *PerformanceMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigration(migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("performance validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema for performance optimization
func (v *PerformanceMigrationValidator) ValidateSchema(ctx context.Context) error {
	// For performance validator, schema validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateChecksums validates migration checksums for performance consistency
func (v *PerformanceMigrationValidator) ValidateChecksums(ctx context.Context) error {
	// For performance validator, checksum validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateDependencies validates migration dependencies for performance compatibility
func (v *PerformanceMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// For performance validator, dependency validation is not applicable
	// This is handled by dependency validator
	return nil
}

// Business Rule Validator Implementation

// NewBusinessRuleMigrationValidator creates a new business rule migration validator
func NewBusinessRuleMigrationValidator() *BusinessRuleMigrationValidator {
	validator := &BusinessRuleMigrationValidator{
		rules: make(map[string]ValidationRule),
	}

	// Add default business rules
	validator.addDefaultRules()

	return validator
}

// AddRule adds a business rule
func (v *BusinessRuleMigrationValidator) AddRule(rule ValidationRule) {
	v.rules[rule.Name] = rule
}

// ValidateMigration validates business rules (interface method)
func (v *BusinessRuleMigrationValidator) ValidateMigration(migration *types.Migration) error {
	return v.ValidateMigrationCtx(context.Background(), migration)
}

// ValidateMigrationCtx validates business rules with context
func (v *BusinessRuleMigrationValidator) ValidateMigrationCtx(ctx context.Context, migration *types.Migration) error {
	for _, rule := range v.rules {
		if err := rule.Validator(migration); err != nil {
			if rule.Severity == SeverityError {
				return fmt.Errorf("business rule violation (%s): %w", rule.Name, err)
			}
		}
	}

	return nil
}

// addDefaultRules adds default business rules
func (v *BusinessRuleMigrationValidator) addDefaultRules() {
	// Rule: Migration must have description
	v.AddRule(ValidationRule{
		Name:        "require_description",
		Description: "Migration must have a description",
		Severity:    SeverityError,
		Validator: func(migration *types.Migration) error {
			if strings.TrimSpace(migration.Description) == "" {
				return fmt.Errorf("migration must have a description")
			}
			return nil
		},
	})

	// Rule: Migration must have author
	v.AddRule(ValidationRule{
		Name:        "require_author",
		Description: "Migration must have an author",
		Severity:    SeverityWarning,
		Validator: func(migration *types.Migration) error {
			if strings.TrimSpace(migration.Author) == "" {
				return fmt.Errorf("migration should have an author")
			}
			return nil
		},
	})

	// Rule: Rollback should be provided for destructive operations
	v.AddRule(ValidationRule{
		Name:        "require_rollback_for_destructive",
		Description: "Destructive operations should have rollback SQL",
		Severity:    SeverityWarning,
		Validator: func(migration *types.Migration) error {
			content := strings.ToUpper(migration.Script)
			destructive := DestructiveOperations

			for _, op := range destructive {
				if strings.Contains(content, op) && migration.RollbackSQL == "" {
					return fmt.Errorf("destructive operation detected but no rollback SQL provided")
				}
			}
			return nil
		},
	})
}

// ValidateMigrations validates multiple migrations for business rules
func (v *BusinessRuleMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckDependencies checks dependencies for business rule validators (no-op)
func (v *BusinessRuleMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	return nil
}

// CheckCircularDependencies checks for circular dependencies (no-op for business rule validator)
func (v *BusinessRuleMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	return nil
}

// ValidateMigrationsCtx validates multiple migrations for business rules with context
func (v *BusinessRuleMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigration(migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("business rule validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema for business rule compliance
func (v *BusinessRuleMigrationValidator) ValidateSchema(ctx context.Context) error {
	// For business rule validator, schema validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateChecksums validates migration checksums for business rule consistency
func (v *BusinessRuleMigrationValidator) ValidateChecksums(ctx context.Context) error {
	// For business rule validator, checksum validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateDependencies validates migration dependencies for business rule compatibility
func (v *BusinessRuleMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// For business rule validator, dependency validation is not applicable
	// This is handled by dependency validator
	return nil
}

// Helper methods

// checkBalancedParentheses checks for balanced parentheses
func (v *SyntaxMigrationValidator) checkBalancedParentheses(content string) error {
	count := 0
	for _, char := range content {
		switch char {
		case '(':
			count++
		case ')':
			count--
			if count < 0 {
				return fmt.Errorf("unbalanced parentheses: extra closing parenthesis")
			}
		}
	}
	if count != 0 {
		return fmt.Errorf("unbalanced parentheses: %d unclosed", count)
	}
	return nil
}

// checkBalancedQuotes checks for balanced quotes
func (v *SyntaxMigrationValidator) checkBalancedQuotes(content string) error {
	singleQuotes := 0
	doubleQuotes := 0

	for _, char := range content {
		switch char {
		case '\'':
			singleQuotes++
		case '"':
			doubleQuotes++
		}
	}

	if singleQuotes%2 != 0 {
		return fmt.Errorf("unbalanced single quotes")
	}
	if doubleQuotes%2 != 0 {
		return fmt.Errorf("unbalanced double quotes")
	}

	return nil
}

// checkSQLKeywords checks for valid SQL keywords
func (v *SyntaxMigrationValidator) checkSQLKeywords(content string) error {
	// This is a simplified check
	// In a real implementation, this would be more comprehensive
	invalidPatterns := []string{
		`(?i)\b(EXEC|EXECUTE)\s+xp_`,
		`(?i)\b(EXEC|EXECUTE)\s+sp_`,
	}

	for _, pattern := range invalidPatterns {
		if matched, _ := regexp.MatchString(pattern, content); matched {
			return fmt.Errorf("potentially dangerous SQL keyword detected: %s", pattern)
		}
	}

	return nil
}

// splitStatements splits SQL content into statements
func (v *SyntaxMigrationValidator) splitStatements(content string) []string {
	var statements []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool
	var inLineComment, inBlockComment bool
	
	lines := strings.Split(content, "\n")
	
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		
		// Skip empty lines and migration directives
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "-- +migrate") {
			continue
		}
		
		// Handle line comments
		if strings.HasPrefix(trimmedLine, "--") {
			continue
		}
		
		// Process character by character to handle quotes and comments properly
		for i, char := range line {
			switch {
			case inLineComment:
				// Skip rest of line if in line comment
				break
			case inBlockComment:
				if char == '*' && i+1 < len(line) && rune(line[i+1]) == '/' {
					inBlockComment = false
					i++ // skip the '/'
				}
			case char == '\'' && !inDoubleQuote:
				inSingleQuote = !inSingleQuote
				current.WriteRune(char)
			case char == '"' && !inSingleQuote:
				inDoubleQuote = !inDoubleQuote
				current.WriteRune(char)
			case char == '-' && i+1 < len(line) && rune(line[i+1]) == '-' && !inSingleQuote && !inDoubleQuote:
				inLineComment = true
				break // Skip rest of line
			case char == '/' && i+1 < len(line) && rune(line[i+1]) == '*' && !inSingleQuote && !inDoubleQuote:
				inBlockComment = true
				i++ // skip the '*'
			case char == ';' && !inSingleQuote && !inDoubleQuote:
				// End of statement
				stmt := strings.TrimSpace(current.String())
				if stmt != "" {
					statements = append(statements, stmt)
				}
				current.Reset()
			default:
				current.WriteRune(char)
			}
		}
		
		// Reset line comment flag at end of line
		inLineComment = false
		
		// Add newline if we're building a statement
		if current.Len() > 0 {
			current.WriteRune('\n')
		}
	}
	
	// Add any remaining statement
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}
	
	return statements
}

// isNonExplainableStatement checks if a statement can be explained
func (v *SyntaxMigrationValidator) isNonExplainableStatement(stmt string) bool {
	upperStmt := strings.ToUpper(strings.TrimSpace(stmt))
	
	// Only skip statements that truly cannot be explained
	// Most DDL statements (CREATE, ALTER, DROP) can be explained
	nonExplainable := []string{
		"PRAGMA",
		"SET",
		"SHOW",
		"USE",
		"COMMIT",
		"ROLLBACK",
		"BEGIN",
		"START TRANSACTION",
	}

	for _, keyword := range nonExplainable {
		if strings.HasPrefix(upperStmt, keyword) {
			return true
		}
	}

	return false
}

// isStatementLine checks if a line contains a SQL statement
func (v *SyntaxMigrationValidator) isDDLStatement(stmt string) bool {
	upperStmt := strings.ToUpper(strings.TrimSpace(stmt))
	
	ddlKeywords := []string{
		"CREATE",
		"ALTER",
		"DROP",
	}

	for _, keyword := range ddlKeywords {
		if strings.HasPrefix(upperStmt, keyword) {
			return true
		}
	}

	return false
}

func (v *SyntaxMigrationValidator) validateSQLiteDDLSyntax(stmt string) error {
	// Basic syntax validation for SQLite DDL statements
	stmt = strings.TrimSpace(stmt)
	upperStmt := strings.ToUpper(stmt)
	
	// Check for basic syntax issues
	if !strings.HasSuffix(stmt, ";") && !strings.HasSuffix(stmt, ")") {
		// Allow statements without semicolon as they might be part of a larger statement
	}
	
	// Check for balanced parentheses
	if err := v.checkBalancedParentheses(stmt); err != nil {
		return err
	}
	
	// Check for balanced quotes
	if err := v.checkBalancedQuotes(stmt); err != nil {
		return err
	}
	
	// Basic keyword validation
	validDDLPatterns := []string{
		"CREATE TABLE",
		"CREATE INDEX",
		"CREATE UNIQUE INDEX",
		"ALTER TABLE",
		"DROP TABLE",
		"DROP INDEX",
	}
	
	isValid := false
	for _, pattern := range validDDLPatterns {
		if strings.HasPrefix(upperStmt, pattern) {
			isValid = true
			break
		}
	}
	
	if !isValid {
		return fmt.Errorf("unsupported DDL statement")
	}
	
	return nil
}

func (v *SyntaxMigrationValidator) isStatementLine(line string) bool {
	if line == "" || strings.HasPrefix(line, "--") || strings.HasPrefix(line, "/*") {
		return false
	}

	upperLine := strings.ToUpper(line)
	keywords := SQLQueryKeywords

	for _, keyword := range keywords {
		if strings.HasPrefix(upperLine, keyword) {
			return true
		}
	}

	return false
}

// ValidateMigrations validates multiple migrations for syntax
func (v *SyntaxMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMigrationsCtx validates multiple migrations for syntax with context
func (v *SyntaxMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigration(migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("syntax validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema for syntax compatibility
func (v *SyntaxMigrationValidator) ValidateSchema(ctx context.Context) error {
	// For syntax validator, we don't need to validate the schema structure
	// This is handled by other validators
	return nil
}

// ValidateChecksums validates migration checksums for syntax consistency
func (v *SyntaxMigrationValidator) ValidateChecksums(ctx context.Context) error {
	// For syntax validator, checksum validation is not applicable
	// This is handled by other validators
	return nil
}

// ValidateDependencies validates migration dependencies for syntax compatibility
func (v *SyntaxMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// For syntax validator, dependency validation is not applicable
	// This is handled by dependency validator
	return nil
}

// ValidateMigrations validates multiple migrations using all registered validators
func (v *CompositeMigrationValidator) ValidateMigrations(migrations []types.Migration) error {
	for _, migration := range migrations {
		if err := v.ValidateMigration(&migration); err != nil {
			return err
		}
	}
	return nil
}

// CheckDependencies checks dependencies using all registered validators
func (v *CompositeMigrationValidator) CheckDependencies(migrations []types.Migration) error {
	for _, validator := range v.validators {
		if err := validator.CheckDependencies(migrations); err != nil {
			return err
		}
	}
	return nil
}

// CheckCircularDependencies checks for circular dependencies using all registered validators
func (v *CompositeMigrationValidator) CheckCircularDependencies(migrations []types.Migration) error {
	for _, validator := range v.validators {
		if err := validator.CheckCircularDependencies(migrations); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMigrationsCtx validates multiple migrations using all registered validators with context
func (v *CompositeMigrationValidator) ValidateMigrationsCtx(ctx context.Context, migrations []*types.Migration) error {
	var allErrors []string

	for _, migration := range migrations {
		if err := v.ValidateMigrationCtx(ctx, migration); err != nil {
			allErrors = append(allErrors, fmt.Sprintf("migration %s: %s", migration.ID, err.Error()))
		}
	}

	if len(allErrors) > 0 {
		return fmt.Errorf("migrations validation failed: %s", strings.Join(allErrors, "; "))
	}

	return nil
}

// ValidateSchema validates the database schema
func (v *CompositeMigrationValidator) ValidateSchema(ctx context.Context) error {
	v.logger.WithContext(ctx).Info("Starting schema validation")
	
	// Check what tables exist before AutoMigrate
	var tablesBefore []string
	if err := v.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tablesBefore).Error; err == nil {
		v.logger.WithContext(ctx).Info("Tables before AutoMigrate", map[string]interface{}{
			"tables": tablesBefore,
			"count":  len(tablesBefore),
		})
	}
	
	// Check if migration tables exist and are properly structured
	if err := v.db.WithContext(ctx).AutoMigrate(&types.Migration{}, &lock.MigrationLock{}, &types.ChangelogEntryRecord{}); err != nil {
		v.logger.WithContext(ctx).Error("Schema validation failed during AutoMigrate", err)
		return fmt.Errorf("schema validation failed: %w", err)
	}
	
	// Check what tables exist after AutoMigrate
	var tablesAfter []string
	if err := v.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tablesAfter).Error; err == nil {
		v.logger.WithContext(ctx).Info("Tables after AutoMigrate", map[string]interface{}{
			"tables": tablesAfter,
			"count":  len(tablesAfter),
		})
	}
	
	v.logger.WithContext(ctx).Info("Schema validation completed successfully")

	// Validate table structure
	if !v.db.WithContext(ctx).Migrator().HasTable(&types.Migration{}) {
		return fmt.Errorf("migration table does not exist")
	}

	if !v.db.WithContext(ctx).Migrator().HasTable(&lock.MigrationLock{}) {
		return fmt.Errorf("migration lock table does not exist")
	}

	if v.config.Migration.EnableChangelog && !v.db.WithContext(ctx).Migrator().HasTable(&types.ChangelogEntryRecord{}) {
		return fmt.Errorf("changelog table does not exist")
	}

	return nil
}

// ValidateChecksum validates a single migration checksum (interface method)
func (v *CompositeMigrationValidator) ValidateChecksum(migration *types.Migration) error {
	if migration.Checksum == "" {
		return fmt.Errorf("migration %s has no checksum", migration.ID)
	}

	// Calculate current checksum and compare
	currentChecksum := v.calculateChecksum(migration.Script)
	if currentChecksum != migration.Checksum {
		return fmt.Errorf("checksum mismatch for migration %s: expected %s, got %s",
			migration.ID, migration.Checksum, currentChecksum)
	}

	return nil
}

// ValidateDependencies validates migration dependencies (interface method)
func (v *CompositeMigrationValidator) ValidateDependencies(ctx context.Context, migrations []*types.Migration) error {
	// Use the dependency validator logic directly
	for _, migration := range migrations {
		// Check if all dependencies exist and are valid
		for _, depID := range migration.Dependencies {
			found := false
			for _, dep := range migrations {
				if dep.ID == depID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("migration %s depends on non-existent migration %s", migration.ID, depID)
			}
		}
	}
	return nil
}

// ValidateChecksums validates migration checksums
func (v *CompositeMigrationValidator) ValidateChecksums(ctx context.Context) error {
	var migrations []*types.Migration
	if err := v.db.WithContext(ctx).Find(&migrations).Error; err != nil {
		return fmt.Errorf("failed to load migrations for checksum validation: %w", err)
	}

	for _, migration := range migrations {
		if migration.State == constants.MigrationStateSuccess {
			// For executed migrations, verify checksum hasn't changed
			if migration.Checksum == "" {
				return fmt.Errorf("migration %s has no checksum", migration.ID)
			}

			// Calculate current checksum and compare
			currentChecksum := v.calculateChecksum(migration.Script)
			if currentChecksum != migration.Checksum {
				return fmt.Errorf("checksum mismatch for migration %s: expected %s, got %s",
					migration.ID, migration.Checksum, currentChecksum)
			}
		}
	}

	return nil
}
