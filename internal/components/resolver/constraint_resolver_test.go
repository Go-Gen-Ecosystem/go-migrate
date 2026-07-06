/**
 * @file constraint_resolver_test.go
 * @description Behavioral tests for the constraint resolver and its per-type
 * validators (version, time, environment, database, conflict, resource).
 * @author Admilson B. F. Cossa
 */

package resolver

import (
	"context"
	"testing"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDefaultConstraintResolver_Defaults(t *testing.T) {
	r := NewDefaultConstraintResolver(nil)
	require.NotNil(t, r)
	assert.False(t, r.config.StrictMode)
	assert.Equal(t, 100, r.config.MaxViolations)
	// Default validators registered for all built-in constraint types.
	assert.NotNil(t, r.validators[ConstraintTypeVersion])
	assert.NotNil(t, r.validators[ConstraintTypeConflict])
}

func TestAddRemoveGetConstraint(t *testing.T) {
	r := NewDefaultConstraintResolver(nil)

	assert.Error(t, r.AddConstraint(&Constraint{Name: ""}), "empty name rejected")

	c := &Constraint{Type: ConstraintTypeVersion, Name: "v-fmt"}
	require.NoError(t, r.AddConstraint(c))
	assert.Len(t, r.GetConstraints(), 1)

	require.NoError(t, r.RemoveConstraint("v-fmt"))
	assert.Empty(t, r.GetConstraints())

	assert.Error(t, r.RemoveConstraint("missing"))
}

func TestValidateConstraints_VersionViolation(t *testing.T) {
	r := NewDefaultConstraintResolver(&ConstraintResolverConfig{
		MaxViolations:      100,
		EnabledConstraints: []ConstraintType{ConstraintTypeVersion},
	})
	require.NoError(t, r.AddConstraint(&Constraint{
		Type: ConstraintTypeVersion,
		Name: "version_format",
	}))

	// Invalid version triggers a violation.
	bad := []*types.Migration{{ID: "m1", Version: "not-semver"}}
	violations, err := r.ValidateConstraints(context.Background(), bad)
	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, SeverityError, violations[0].Severity)
	assert.Contains(t, violations[0].Message, "Invalid version format")

	// Valid semantic version yields no violation.
	good := []*types.Migration{{ID: "m2", Version: "v1.2.3"}}
	violations, err = r.ValidateConstraints(context.Background(), good)
	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestValidateConstraints_DisabledTypeSkipped(t *testing.T) {
	// The constraint type is registered but NOT enabled, so it is skipped.
	r := NewDefaultConstraintResolver(&ConstraintResolverConfig{
		MaxViolations:      100,
		EnabledConstraints: []ConstraintType{ConstraintTypeTime}, // version not enabled
	})
	require.NoError(t, r.AddConstraint(&Constraint{
		Type: ConstraintTypeVersion,
		Name: "version_format",
	}))

	bad := []*types.Migration{{ID: "m1", Version: "not-semver"}}
	violations, err := r.ValidateConstraints(context.Background(), bad)
	require.NoError(t, err)
	assert.Empty(t, violations, "disabled constraint type must be skipped")
}

func TestTimeConstraintValidator(t *testing.T) {
	v := &TimeConstraintValidator{}
	c := &Constraint{Type: ConstraintTypeTime, Name: "t"}
	cfg := &ConstraintResolverConfig{}

	// Zero timestamp => error violation.
	zero := &types.Migration{ID: "m"}
	violation, err := v.Validate(context.Background(), c, zero, cfg)
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityError, violation.Severity)

	// Future timestamp => warning violation.
	future := &types.Migration{ID: "m", CreatedAt: time.Now().Add(48 * time.Hour)}
	violation, err = v.Validate(context.Background(), c, future, cfg)
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityWarning, violation.Severity)

	// Present timestamp => no violation.
	now := &types.Migration{ID: "m", CreatedAt: time.Now()}
	violation, err = v.Validate(context.Background(), c, now, cfg)
	require.NoError(t, err)
	assert.Nil(t, violation)
}

func TestEnvironmentConstraintValidator(t *testing.T) {
	v := &EnvironmentConstraintValidator{}
	c := &Constraint{
		Type: ConstraintTypeEnvironment,
		Name: "env",
		Parameters: map[string]interface{}{
			"allowed_environments": []string{"staging", "production"},
		},
	}

	// Disallowed environment => error.
	violation, err := v.Validate(context.Background(), c, &types.Migration{ID: "m"},
		&ConstraintResolverConfig{Environment: "development"})
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityError, violation.Severity)

	// Allowed environment => no violation.
	violation, err = v.Validate(context.Background(), c, &types.Migration{ID: "m"},
		&ConstraintResolverConfig{Environment: "production"})
	require.NoError(t, err)
	assert.Nil(t, violation)
}

func TestDatabaseConstraintValidator(t *testing.T) {
	v := &DatabaseConstraintValidator{}
	c := &Constraint{
		Type: ConstraintTypeDatabase,
		Name: "db",
		Parameters: map[string]interface{}{
			"supported_databases": []string{"postgres", "mysql"},
		},
	}

	// Unsupported database => error.
	violation, err := v.Validate(context.Background(), c, &types.Migration{ID: "m"},
		&ConstraintResolverConfig{DatabaseType: "oracle"})
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityError, violation.Severity)

	// Supported database (case-insensitive) => no violation.
	violation, err = v.Validate(context.Background(), c, &types.Migration{ID: "m"},
		&ConstraintResolverConfig{DatabaseType: "PostgreS"})
	require.NoError(t, err)
	assert.Nil(t, violation)
}

func TestConflictConstraintValidator(t *testing.T) {
	v := &ConflictConstraintValidator{}
	c := &Constraint{
		Type: ConstraintTypeConflict,
		Name: "conflict",
		Parameters: map[string]interface{}{
			"conflict_patterns": []string{`DROP\s+TABLE`},
		},
	}

	// Matching pattern => warning violation.
	violation, err := v.Validate(context.Background(), c,
		&types.Migration{ID: "m", Script: "DROP TABLE users;"}, &ConstraintResolverConfig{})
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityWarning, violation.Severity)

	// No match => no violation.
	violation, err = v.Validate(context.Background(), c,
		&types.Migration{ID: "m", Script: "CREATE TABLE users (id INT);"}, &ConstraintResolverConfig{})
	require.NoError(t, err)
	assert.Nil(t, violation)
}

func TestResourceConstraintValidator(t *testing.T) {
	v := &ResourceConstraintValidator{}
	c := &Constraint{
		Type:       ConstraintTypeResource,
		Name:       "size",
		Parameters: map[string]interface{}{"max_sql_size": 10},
	}

	// Oversized SQL => warning.
	violation, err := v.Validate(context.Background(), c,
		&types.Migration{ID: "m", Script: "this script is definitely longer than ten"},
		&ConstraintResolverConfig{})
	require.NoError(t, err)
	require.NotNil(t, violation)
	assert.Equal(t, SeverityWarning, violation.Severity)

	// Within limit => no violation.
	violation, err = v.Validate(context.Background(), c,
		&types.Migration{ID: "m", Script: "short"}, &ConstraintResolverConfig{})
	require.NoError(t, err)
	assert.Nil(t, violation)
}

func TestVersionConstraintValidator_Type(t *testing.T) {
	assert.Equal(t, ConstraintTypeVersion, (&VersionConstraintValidator{}).GetType())
	assert.Equal(t, ConstraintTypeTime, (&TimeConstraintValidator{}).GetType())
	assert.Equal(t, ConstraintTypeEnvironment, (&EnvironmentConstraintValidator{}).GetType())
	assert.Equal(t, ConstraintTypeDatabase, (&DatabaseConstraintValidator{}).GetType())
	assert.Equal(t, ConstraintTypeConflict, (&ConflictConstraintValidator{}).GetType())
	assert.Equal(t, ConstraintTypeResource, (&ResourceConstraintValidator{}).GetType())
}

func TestValidateConstraints_MaxViolationsCap(t *testing.T) {
	r := NewDefaultConstraintResolver(&ConstraintResolverConfig{
		MaxViolations:      1,
		EnabledConstraints: []ConstraintType{ConstraintTypeVersion},
	})
	require.NoError(t, r.AddConstraint(&Constraint{Type: ConstraintTypeVersion, Name: "v"}))

	migrations := []*types.Migration{
		{ID: "m1", Version: "bad1"},
		{ID: "m2", Version: "bad2"},
		{ID: "m3", Version: "bad3"},
	}
	violations, err := r.ValidateConstraints(context.Background(), migrations)
	require.NoError(t, err)
	// The loop breaks once the cap is reached.
	assert.Len(t, violations, 1)
}
