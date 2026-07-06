/**
 * @file placeholders_engine_test.go
 * @description Unit tests for the placeholder migration engine and the
 *              components-factory placeholder methods not covered elsewhere.
 *              The placeholder engine implements the full engine contract with
 *              deterministic not-implemented behavior; these tests verify the
 *              connection lifecycle flag, the not-yet-implemented operation
 *              results, and metadata accessors.
 * @author Admilson B. F. Cossa
 * @date 2025-07-06
 * @path internal/components/factory/placeholders_engine_test.go
 */

package factory

import (
	"context"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

func TestPlaceholderComponentsFactory_NewRollbackExecutor(t *testing.T) {
	pcf := &PlaceholderComponentsFactory{dialect: "test"}

	executor, err := pcf.NewRollbackExecutor(types.Config{})
	if err == nil {
		t.Error("expected error from placeholder NewRollbackExecutor, got nil")
	}
	if executor != nil {
		t.Error("expected nil rollback executor from placeholder")
	}
}

func TestPlaceholderMigrationEngine_ConnectionLifecycle(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}
	ctx := context.Background()

	if pme.IsConnected() {
		t.Error("expected engine to start disconnected")
	}

	if err := pme.Connect(ctx); err != nil {
		t.Fatalf("Connect returned unexpected error: %v", err)
	}
	if !pme.IsConnected() {
		t.Error("expected engine to be connected after Connect")
	}

	if err := pme.Disconnect(ctx); err != nil {
		t.Fatalf("Disconnect returned unexpected error: %v", err)
	}
	if pme.IsConnected() {
		t.Error("expected engine to be disconnected after Disconnect")
	}
}

func TestPlaceholderMigrationEngine_Up_NotImplemented(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	result, err := pme.Up(context.Background(), []types.MigrationScript{})
	if err != nil {
		t.Fatalf("Up returned an error; expected result-encoded failure: %v", err)
	}
	if result.Success {
		t.Error("expected placeholder Up to report failure")
	}
	if len(result.Errors) == 0 {
		t.Error("expected placeholder Up to include an error message")
	}
}

func TestPlaceholderMigrationEngine_Down_NotImplemented(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	result, err := pme.Down(context.Background(), "v1")
	if err != nil {
		t.Fatalf("Down returned an error; expected result-encoded failure: %v", err)
	}
	if result.Success {
		t.Error("expected placeholder Down to report failure")
	}
	if len(result.Errors) == 0 {
		t.Error("expected placeholder Down to include an error message")
	}
}

func TestPlaceholderMigrationEngine_Status_NotImplemented(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	status, err := pme.Status(context.Background())
	if err == nil {
		t.Error("expected placeholder Status to return an error")
	}
	if status != nil {
		t.Error("expected nil status from placeholder")
	}
}

func TestPlaceholderMigrationEngine_DryRun_NotImplemented(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	result, err := pme.DryRun(context.Background(), []types.MigrationScript{})
	if err != nil {
		t.Fatalf("DryRun returned an error; expected result-encoded failure: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected placeholder DryRun to include an error message")
	}
}

func TestPlaceholderMigrationEngine_Validate_NotImplemented(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	result, err := pme.Validate(context.Background(), []types.MigrationScript{})
	if err != nil {
		t.Fatalf("Validate returned an error; expected result-encoded failure: %v", err)
	}
	if result.Valid {
		t.Error("expected placeholder Validate to report invalid")
	}
	if result.ErrorCount == 0 {
		t.Error("expected placeholder Validate to report at least one error")
	}
}

func TestPlaceholderMigrationEngine_Metadata(t *testing.T) {
	pme := &PlaceholderMigrationEngine{engineType: "test"}

	if pme.GetEngineType() != "test" {
		t.Errorf("expected engine type 'test', got %q", pme.GetEngineType())
	}

	features := pme.GetSupportedFeatures()
	if len(features) == 0 {
		t.Error("expected placeholder to report at least one feature")
	}
}
