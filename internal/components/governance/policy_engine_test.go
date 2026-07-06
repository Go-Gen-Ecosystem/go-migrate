/**
 * @file policy_engine_test.go
 * @description Tests for policy engine
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/components/governance/policy_engine_test.go
 */

package governance

import (
	"context"
	"testing"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPolicyEngine_NoDropTableInProduction(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		script      string
		shouldBlock bool
	}{
		{
			name:        "drop table in production should be blocked",
			environment: "production",
			script:      "DROP TABLE users;",
			shouldBlock: true,
		},
		{
			name:        "drop table in development should pass",
			environment: "development",
			script:      "DROP TABLE users;",
			shouldBlock: false,
		},
		{
			name:        "drop schema in production should be blocked",
			environment: "production",
			script:      "DROP SCHEMA public;",
			shouldBlock: true,
		},
		{
			name:        "create table in production should pass",
			environment: "production",
			script:      "CREATE TABLE users (id INT);",
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewPolicyEngine(PolicyEngineConfig{
				Environment: tt.environment,
			})
			engine.AddPolicy(NewNoDropTableInProductionPolicy(tt.environment))

			migration := &types.Migration{
				ID:     "test-migration",
				Script: tt.script,
			}

			result, err := engine.Evaluate(context.Background(), migration)
			require.NoError(t, err)

			if tt.shouldBlock {
				assert.False(t, result.Passed)
				assert.Equal(t, PolicyActionBlock, result.Action)
				assert.Greater(t, len(result.Violations), 0)
			} else {
				assert.True(t, result.Passed)
			}
		})
	}
}

func TestPolicyEngine_DestructiveOperations(t *testing.T) {
	engine := NewPolicyEngine(PolicyEngineConfig{})
	engine.AddPolicy(NewDestructiveOperationPolicy())

	tests := []struct {
		name              string
		script            string
		shouldRequireApproval bool
	}{
		{
			name:              "drop table requires approval",
			script:            "DROP TABLE users;",
			shouldRequireApproval: true,
		},
		{
			name:              "delete requires approval",
			script:            "DELETE FROM users WHERE id = 1;",
			shouldRequireApproval: true,
		},
		{
			name:              "truncate requires approval",
			script:            "TRUNCATE TABLE users;",
			shouldRequireApproval: true,
		},
		{
			name:              "alter table requires approval",
			script:            "ALTER TABLE users DROP COLUMN email;",
			shouldRequireApproval: true,
		},
		{
			name:              "create table does not require approval",
			script:            "CREATE TABLE users (id INT);",
			shouldRequireApproval: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &types.Migration{
				ID:     "test-migration",
				Script: tt.script,
			}

			result, err := engine.Evaluate(context.Background(), migration)
			require.NoError(t, err)

			if tt.shouldRequireApproval {
				assert.False(t, result.Passed)
				assert.True(t, result.RequiresApproval)
				assert.Equal(t, PolicyActionRequire, result.Action)
			} else {
				assert.True(t, result.Passed)
				assert.False(t, result.RequiresApproval)
			}
		})
	}
}

func TestPolicyEngine_RequireRollback(t *testing.T) {
	engine := NewPolicyEngine(PolicyEngineConfig{})
	engine.AddPolicy(NewRequireRollbackPolicy())

	tests := []struct {
		name        string
		script      string
		canRollback bool
		rollbackSQL string
		shouldFail  bool
	}{
		{
			name:        "destructive without rollback should fail",
			script:      "DROP TABLE users;",
			canRollback: false,
			rollbackSQL: "",
			shouldFail:  true,
		},
		{
			name:        "destructive with rollback should pass",
			script:      "DROP TABLE users;",
			canRollback: true,
			rollbackSQL: "CREATE TABLE users (id INT);",
			shouldFail:  false,
		},
		{
			name:        "non-destructive without rollback should pass",
			script:      "CREATE TABLE users (id INT);",
			canRollback: false,
			rollbackSQL: "",
			shouldFail:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &types.Migration{
				ID:          "test-migration",
				Script:      tt.script,
				CanRollback: tt.canRollback,
				RollbackSQL: tt.rollbackSQL,
			}

			result, err := engine.Evaluate(context.Background(), migration)
			require.NoError(t, err)

			if tt.shouldFail {
				assert.False(t, result.Passed)
				assert.Equal(t, PolicyActionBlock, result.Action)
			} else {
				assert.True(t, result.Passed)
			}
		})
	}
}

func TestPolicyEngine_NoHardcodedCredentials(t *testing.T) {
	engine := NewPolicyEngine(PolicyEngineConfig{})
	engine.AddPolicy(NewNoHardcodedCredentialsPolicy())

	tests := []struct {
		name       string
		script     string
		shouldFail bool
	}{
		{
			name:       "hardcoded password should be blocked",
			script:     "CREATE USER admin WITH PASSWORD 'secret123';",
			shouldFail: true,
		},
		{
			name:       "hardcoded secret should be blocked",
			script:     "INSERT INTO config (secret = 'my-secret-key');",
			shouldFail: true,
		},
		{
			name:       "normal SQL should pass",
			script:     "CREATE TABLE users (id INT);",
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &types.Migration{
				ID:     "test-migration",
				Script: tt.script,
			}

			result, err := engine.Evaluate(context.Background(), migration)
			require.NoError(t, err)

			if tt.shouldFail {
				assert.False(t, result.Passed)
				assert.Equal(t, PolicyActionBlock, result.Action)
			} else {
				assert.True(t, result.Passed)
			}
		})
	}
}

func TestPolicyEngine_RequireMetadata(t *testing.T) {
	engine := NewPolicyEngine(PolicyEngineConfig{})
	engine.AddPolicy(NewRequireMetadataPolicy())

	tests := []struct {
		name        string
		description string
		author      string
		shouldWarn  bool
	}{
		{
			name:        "missing description should warn",
			description: "",
			author:      "john.doe",
			shouldWarn:  true,
		},
		{
			name:        "missing author should warn",
			description: "Add users table",
			author:      "",
			shouldWarn:  true,
		},
		{
			name:        "complete metadata should pass",
			description: "Add users table",
			author:      "john.doe",
			shouldWarn:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &types.Migration{
				ID:          "test-migration",
				Description: tt.description,
				Author:      tt.author,
				Script:      "CREATE TABLE users (id INT);",
			}

			result, err := engine.Evaluate(context.Background(), migration)
			require.NoError(t, err)

			if tt.shouldWarn {
				assert.False(t, result.Passed)
				assert.Equal(t, PolicyActionWarn, result.Action)
			} else {
				assert.True(t, result.Passed)
			}
		})
	}
}

func TestPolicyEngine_StrictMode(t *testing.T) {
	// Strict mode should convert warnings to blocks
	engine := NewPolicyEngine(PolicyEngineConfig{
		StrictMode: true,
	})
	engine.AddPolicy(NewRequireMetadataPolicy())

	migration := &types.Migration{
		ID:          "test-migration",
		Description: "", // Missing description
		Script:      "CREATE TABLE users (id INT);",
	}

	result, err := engine.Evaluate(context.Background(), migration)
	require.NoError(t, err)

	assert.False(t, result.Passed)
	assert.Equal(t, PolicyActionBlock, result.Action)
}
