/**
 * @file auto_generator_test.go
 * @description Tests for the enhanced automatic rollback script generator
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/rollback/auto_generator_test.go
 */

package rollback

import (
	"context"
	"strings"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestAutoRollbackGenerator_GenerateRollback(t *testing.T) {
	tests := []struct {
		name           string
		migration      *types.Migration
		config         *AutoRollbackConfig
		expectedOps    int
		expectedErrors int
		shouldComplete bool
	}{
		{
			name: "Simple CREATE TABLE",
			migration: &types.Migration{
				ID:      "001",
				Version: "1.0.0",
				Script:  "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(100));",
				Type:    types.MigrationTypeSQL,
			},
			config:         nil, // Use default config
			expectedOps:    1,
			expectedErrors: 0,
			shouldComplete: true,
		},
		{
			name: "Multiple DDL operations",
			migration: &types.Migration{
				ID:      "002",
				Version: "1.0.1",
				Script: `
					CREATE TABLE products (id INT PRIMARY KEY, name VARCHAR(100));
					CREATE INDEX idx_products_name ON products(name);
					ALTER TABLE products ADD COLUMN price DECIMAL(10,2);
				`,
				Type: types.MigrationTypeSQL,
			},
			config:         nil,
			expectedOps:    3,
			expectedErrors: 0,
			shouldComplete: true,
		},
		{
			name: "DROP operations requiring manual intervention",
			migration: &types.Migration{
				ID:      "003",
				Version: "1.0.2",
				Script: `
					DROP TABLE old_users;
					ALTER TABLE products DROP COLUMN description;
				`,
				Type: types.MigrationTypeSQL,
			},
			config:         nil,
			expectedOps:    2,
			expectedErrors: 0,
			shouldComplete: false, // Requires manual intervention
		},
		{
			name: "Data operations with rollbacks disabled",
			migration: &types.Migration{
				ID:      "004",
				Version: "1.0.3",
				Script: `
					INSERT INTO users (id, name) VALUES (1, 'John Doe');
					UPDATE products SET price = 19.99 WHERE id = 1;
					DELETE FROM old_products WHERE created_at < '2020-01-01';
				`,
				Type: types.MigrationTypeSQL,
			},
			config: &AutoRollbackConfig{
				GenerateDataRollbacks: false,
				IncludeWarnings:       true,
			},
			expectedOps:    3,
			expectedErrors: 0,
			shouldComplete: false,
		},
		{
			name: "Complex SQL with comments",
			migration: &types.Migration{
				ID:      "005",
				Version: "1.0.4",
				Script: `
					-- Create users table
					CREATE TABLE users (
						id INT PRIMARY KEY,
						name VARCHAR(100) NOT NULL,
						email VARCHAR(255) UNIQUE
					);
					
					/* Add index for performance */
					CREATE INDEX idx_users_email ON users(email);
				`,
				Type: types.MigrationTypeSQL,
			},
			config:         nil,
			expectedOps:    2,
			expectedErrors: 0,
			shouldComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewAutoRollbackGenerator("postgresql", tt.config)
			
			rollback, err := generator.GenerateRollback(context.Background(), tt.migration)
			if err != nil {
				t.Fatalf("GenerateRollback() error = %v", err)
			}

			if len(rollback.Operations) != tt.expectedOps {
				t.Errorf("Expected %d operations, got %d", tt.expectedOps, len(rollback.Operations))
			}

			if len(rollback.Errors) != tt.expectedErrors {
				t.Errorf("Expected %d errors, got %d", tt.expectedErrors, len(rollback.Errors))
			}

			if rollback.IsComplete != tt.shouldComplete {
				t.Errorf("Expected IsComplete = %v, got %v", tt.shouldComplete, rollback.IsComplete)
			}

			// Verify rollback script is not empty
			if rollback.FullScript == "" {
				t.Error("Expected non-empty rollback script")
			}

			// Verify metadata
			if rollback.MigrationID != tt.migration.ID {
				t.Errorf("Expected MigrationID = %s, got %s", tt.migration.ID, rollback.MigrationID)
			}

			if rollback.Version != tt.migration.Version {
				t.Errorf("Expected Version = %s, got %s", tt.migration.Version, rollback.Version)
			}
		})
	}
}

func TestAutoRollbackGenerator_ParseSQL(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		expected []string
	}{
		{
			name:   "Single statement",
			script: "CREATE TABLE users (id INT PRIMARY KEY);",
			expected: []string{
				"CREATE TABLE users (id INT PRIMARY KEY)",
			},
		},
		{
			name: "Multiple statements",
			script: `
				CREATE TABLE users (id INT PRIMARY KEY);
				CREATE INDEX idx_users_id ON users(id);
				ALTER TABLE users ADD COLUMN name VARCHAR(100);
			`,
			expected: []string{
				"CREATE TABLE users (id INT PRIMARY KEY)",
				"CREATE INDEX idx_users_id ON users(id)",
				"ALTER TABLE users ADD COLUMN name VARCHAR(100)",
			},
		},
		{
			name: "Statements with comments",
			script: `
				-- Create users table
				CREATE TABLE users (id INT PRIMARY KEY);
				/* Add index */
				CREATE INDEX idx_users_id ON users(id);
			`,
			expected: []string{
				"CREATE TABLE users (id INT PRIMARY KEY)",
				"CREATE INDEX idx_users_id ON users(id)",
			},
		},
		{
			name: "Statements with string literals containing semicolons",
			script: `
				INSERT INTO config (key, value) VALUES ('db.url', 'jdbc:postgresql://localhost:5432/db');
				UPDATE settings SET description = 'Format: key=value; separated by semicolons';
			`,
			expected: []string{
				"INSERT INTO config (key, value) VALUES ('db.url', 'jdbc:postgresql://localhost:5432/db')",
				"UPDATE settings SET description = 'Format: key=value; separated by semicolons'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewAutoRollbackGenerator("postgresql", nil)
			
			statements, err := generator.parseSQL(tt.script)
			if err != nil {
				t.Fatalf("parseSQL() error = %v", err)
			}

			if len(statements) != len(tt.expected) {
				t.Errorf("Expected %d statements, got %d", len(tt.expected), len(statements))
				t.Errorf("Expected: %v", tt.expected)
				t.Errorf("Got: %v", statements)
				return
			}

			for i, stmt := range statements {
				if strings.TrimSpace(stmt) != strings.TrimSpace(tt.expected[i]) {
					t.Errorf("Statement %d: expected %q, got %q", i, tt.expected[i], stmt)
				}
			}
		})
	}
}

func TestAutoRollbackGenerator_GenerateRollbackOperation(t *testing.T) {
	tests := []struct {
		name           string
		statement      string
		expectedType   string
		expectedSQL    string
		isReversible   bool
		riskLevel      string
		requiresBackup bool
	}{
		{
			name:           "CREATE TABLE",
			statement:      "CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(100))",
			expectedType:   "DROP_TABLE",
			expectedSQL:    "DROP TABLE IF EXISTS USERS;",
			isReversible:   true,
			riskLevel:      "LOW",
			requiresBackup: false,
		},
		{
			name:           "DROP TABLE",
			statement:      "DROP TABLE old_users",
			expectedType:   "CREATE_TABLE",
			expectedSQL:    "-- Cannot automatically recreate table OLD_USERS - manual intervention required",
			isReversible:   false,
			riskLevel:      "HIGH",
			requiresBackup: true,
		},
		{
			name:           "ADD COLUMN",
			statement:      "ALTER TABLE users ADD COLUMN email VARCHAR(255)",
			expectedType:   "DROP_COLUMN",
			expectedSQL:    "ALTER TABLE USERS DROP COLUMN EMAIL;",
			isReversible:   true,
			riskLevel:      "MEDIUM",
			requiresBackup: false,
		},
		{
			name:           "DROP COLUMN",
			statement:      "ALTER TABLE users DROP COLUMN temp_field",
			expectedType:   "ADD_COLUMN",
			expectedSQL:    "-- Cannot automatically recreate column TEMP_FIELD in table USERS - manual intervention required",
			isReversible:   false,
			riskLevel:      "HIGH",
			requiresBackup: true,
		},
		{
			name:           "CREATE INDEX",
			statement:      "CREATE INDEX idx_users_email ON users(email)",
			expectedType:   "DROP_INDEX",
			expectedSQL:    "DROP INDEX IF EXISTS IDX_USERS_EMAIL;",
			isReversible:   true,
			riskLevel:      "LOW",
			requiresBackup: false,
		},
		{
			name:           "DROP INDEX",
			statement:      "DROP INDEX idx_old_users_name",
			expectedType:   "CREATE_INDEX",
			expectedSQL:    "-- Cannot automatically recreate index IDX_OLD_USERS_NAME - manual intervention required",
			isReversible:   false,
			riskLevel:      "HIGH",
			requiresBackup: true,
		},
		{
			name:           "ADD CONSTRAINT",
			statement:      "ALTER TABLE users ADD CONSTRAINT chk_email CHECK (email LIKE '%@%')",
			expectedType:   "DROP_CONSTRAINT",
			expectedSQL:    "ALTER TABLE USERS DROP CONSTRAINT CHK_EMAIL;",
			isReversible:   true,
			riskLevel:      "MEDIUM",
			requiresBackup: false,
		},
		{
			name:           "DROP CONSTRAINT",
			statement:      "ALTER TABLE users DROP CONSTRAINT old_constraint",
			expectedType:   "ADD_CONSTRAINT",
			expectedSQL:    "-- Cannot automatically recreate constraint OLD_CONSTRAINT on table USERS - manual intervention required",
			isReversible:   false,
			riskLevel:      "HIGH",
			requiresBackup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewAutoRollbackGenerator("postgresql", nil)
			
			operation, err := generator.generateRollbackOperation(tt.statement)
			if err != nil {
				t.Fatalf("generateRollbackOperation() error = %v", err)
			}

			if operation.Type != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType, operation.Type)
			}

			if operation.SQL != tt.expectedSQL {
				t.Errorf("Expected SQL %q, got %q", tt.expectedSQL, operation.SQL)
			}

			if operation.IsReversible != tt.isReversible {
				t.Errorf("Expected IsReversible %v, got %v", tt.isReversible, operation.IsReversible)
			}

			if operation.RiskLevel != tt.riskLevel {
				t.Errorf("Expected RiskLevel %s, got %s", tt.riskLevel, operation.RiskLevel)
			}

			if operation.RequiresBackup != tt.requiresBackup {
				t.Errorf("Expected RequiresBackup %v, got %v", tt.requiresBackup, operation.RequiresBackup)
			}
		})
	}
}

func TestAutoRollbackGenerator_ExtractTableName(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		operation string
		expected  string
	}{
		{
			name:      "CREATE TABLE",
			statement: "CREATE TABLE users (id INT PRIMARY KEY)",
			operation: "CREATE TABLE",
			expected:  "USERS",
		},
		{
			name:      "CREATE TABLE IF NOT EXISTS",
			statement: "CREATE TABLE IF NOT EXISTS products (id INT)",
			operation: "CREATE TABLE",
			expected:  "PRODUCTS",
		},
		{
			name:      "DROP TABLE",
			statement: "DROP TABLE old_users",
			operation: "DROP TABLE",
			expected:  "OLD_USERS",
		},
		{
			name:      "DROP TABLE IF EXISTS",
			statement: "DROP TABLE IF EXISTS temp_table",
			operation: "DROP TABLE",
			expected:  "TEMP_TABLE",
		},
		{
			name:      "INSERT INTO",
			statement: "INSERT INTO users (id, name) VALUES (1, 'John')",
			operation: "INSERT INTO",
			expected:  "USERS",
		},
		{
			name:      "UPDATE",
			statement: "UPDATE products SET price = 19.99 WHERE id = 1",
			operation: "UPDATE",
			expected:  "PRODUCTS",
		},
		{
			name:      "DELETE FROM",
			statement: "DELETE FROM old_records WHERE created_at < '2020-01-01'",
			operation: "DELETE FROM",
			expected:  "OLD_RECORDS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewAutoRollbackGenerator("postgresql", nil)
			
			result := generator.extractTableName(tt.statement, tt.operation)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestAutoRollbackGenerator_ExtractIndexName(t *testing.T) {
	tests := []struct {
		name      string
		statement string
		expected  string
	}{
		{
			name:      "CREATE INDEX",
			statement: "CREATE INDEX idx_users_email ON users(email)",
			expected:  "IDX_USERS_EMAIL",
		},
		{
			name:      "CREATE UNIQUE INDEX",
			statement: "CREATE UNIQUE INDEX idx_products_sku ON products(sku)",
			expected:  "IDX_PRODUCTS_SKU",
		},
		{
			name:      "DROP INDEX",
			statement: "DROP INDEX idx_old_users_name",
			expected:  "IDX_OLD_USERS_NAME",
		},
		{
			name:      "DROP INDEX IF EXISTS",
			statement: "DROP INDEX IF EXISTS idx_temp_index",
			expected:  "IDX_TEMP_INDEX",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generator := NewAutoRollbackGenerator("postgresql", nil)
			
			result := generator.extractIndexName(tt.statement)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestAutoRollbackGenerator_BuildFullScript(t *testing.T) {
	operations := []RollbackOperation{
		{
			Type:         "DROP_TABLE",
			SQL:          "DROP TABLE IF EXISTS users;",
			TableName:    "users",
			IsReversible: true,
			RiskLevel:    "LOW",
			Warnings:     []string{},
		},
		{
			Type:         "DROP_INDEX",
			SQL:          "DROP INDEX IF EXISTS idx_users_email;",
			IsReversible: true,
			RiskLevel:    "LOW",
			Warnings:     []string{},
			Metadata:     map[string]string{"index_name": "idx_users_email"},
		},
		{
			Type:           "CREATE_TABLE",
			SQL:            "-- Cannot automatically recreate table old_users - manual intervention required",
			TableName:      "old_users",
			IsReversible:   false,
			RequiresBackup: true,
			RiskLevel:      "HIGH",
			Warnings:       []string{"Cannot automatically recreate dropped table - manual intervention required"},
		},
	}

	generator := NewAutoRollbackGenerator("postgresql", nil)
	script := generator.buildFullScript(operations)

	// Verify script contains expected elements
	if !strings.Contains(script, "Automatic Rollback Script") {
		t.Error("Script should contain header")
	}

	if !strings.Contains(script, "DROP TABLE IF EXISTS users;") {
		t.Error("Script should contain first operation")
	}

	if !strings.Contains(script, "DROP INDEX IF EXISTS idx_users_email;") {
		t.Error("Script should contain second operation")
	}

	if !strings.Contains(script, "manual intervention required") {
		t.Error("Script should contain manual intervention warning")
	}

	if !strings.Contains(script, "Risk Level: HIGH") {
		t.Error("Script should contain risk level information")
	}
}

func TestAutoRollbackGenerator_DataOperations(t *testing.T) {
	tests := []struct {
		name                  string
		statement             string
		generateDataRollbacks bool
		expectedType          string
		shouldBeReversible    bool
	}{
		{
			name:                  "INSERT with data rollbacks enabled",
			statement:             "INSERT INTO users (id, name) VALUES (1, 'John Doe')",
			generateDataRollbacks: true,
			expectedType:          "DELETE_DATA",
			shouldBeReversible:    false, // Still complex to implement safely
		},
		{
			name:                  "INSERT with data rollbacks disabled",
			statement:             "INSERT INTO users (id, name) VALUES (1, 'John Doe')",
			generateDataRollbacks: false,
			expectedType:          "DELETE_DATA",
			shouldBeReversible:    false,
		},
		{
			name:                  "UPDATE with data rollbacks enabled",
			statement:             "UPDATE products SET price = 19.99 WHERE id = 1",
			generateDataRollbacks: true,
			expectedType:          "UPDATE_DATA",
			shouldBeReversible:    false,
		},
		{
			name:                  "DELETE with data rollbacks enabled",
			statement:             "DELETE FROM old_records WHERE created_at < '2020-01-01'",
			generateDataRollbacks: true,
			expectedType:          "INSERT_DATA",
			shouldBeReversible:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AutoRollbackConfig{
				GenerateDataRollbacks: tt.generateDataRollbacks,
				IncludeWarnings:       true,
			}
			
			generator := NewAutoRollbackGenerator("postgresql", config)
			
			operation, err := generator.generateRollbackOperation(tt.statement)
			if err != nil {
				t.Fatalf("generateRollbackOperation() error = %v", err)
			}

			if operation.Type != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType, operation.Type)
			}

			if operation.IsReversible != tt.shouldBeReversible {
				t.Errorf("Expected IsReversible %v, got %v", tt.shouldBeReversible, operation.IsReversible)
			}

			// Data operations should always be high risk
			if operation.RiskLevel != "HIGH" {
				t.Errorf("Expected RiskLevel HIGH for data operations, got %s", operation.RiskLevel)
			}

			// Should have warnings
			if len(operation.Warnings) == 0 {
				t.Error("Expected warnings for data operations")
			}
		})
	}
}

func TestAutoRollbackGenerator_ValidationAndCompletion(t *testing.T) {
	migration := &types.Migration{
		ID:      "test",
		Version: "1.0.0",
		Script: `
			CREATE TABLE users (id INT PRIMARY KEY);
			DROP TABLE old_users;
			ALTER TABLE products ADD COLUMN price DECIMAL(10,2);
			ALTER TABLE products DROP COLUMN description;
		`,
		Type: types.MigrationTypeSQL,
	}

	generator := NewAutoRollbackGenerator("postgresql", nil)
	rollback, err := generator.GenerateRollback(context.Background(), migration)
	if err != nil {
		t.Fatalf("GenerateRollback() error = %v", err)
	}

	// Should not be complete due to DROP operations
	if rollback.IsComplete {
		t.Error("Expected rollback to be incomplete due to DROP operations")
	}

	// Should require manual intervention
	if !rollback.RequiresManual {
		t.Error("Expected rollback to require manual intervention")
	}

	// Should have warnings
	if len(rollback.Warnings) == 0 {
		t.Error("Expected warnings for high-risk operations")
	}

	// Should have operations
	if len(rollback.Operations) != 4 {
		t.Errorf("Expected 4 operations, got %d", len(rollback.Operations))
	}

	// Verify risk assessment
	highRiskCount := 0
	for _, op := range rollback.Operations {
		if op.RiskLevel == "HIGH" {
			highRiskCount++
		}
	}

	if highRiskCount == 0 {
		t.Error("Expected at least one high-risk operation")
	}
}
