/**
 * @file loader_adapter.go
 * @description Adapter to bridge loader.FileMigrationLoader to engine.MigrationLoader interface
 * @author Admilson B. F. Cossa
 * @date 2025-01-28
 * @path pkg/core/database/migrations/internal/adapters/loader_adapter.go
 */

package adapters

import (
    "fmt"

    loaderpkg "github.com/go-gen-ecosystem/go-migrate/internal/components/loader"
    "github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// LoaderAdapter adapts loader.FileMigrationLoader to engine.MigrationLoader
type LoaderAdapter struct {
    loader *loaderpkg.FileMigrationLoader
}

// NewLoaderAdapter creates a new adapter for the file-based migration loader
func NewLoaderAdapter(loader *loaderpkg.FileMigrationLoader) *LoaderAdapter {
    return &LoaderAdapter{loader: loader}
}

// LoadMigrations loads all migrations from path and converts to slice of values
func (a *LoaderAdapter) LoadMigrations(path string) ([]types.Migration, error) {
    migrations, err := a.loader.LoadMigrations(path)
    if err != nil {
        return nil, err
    }
    result := make([]types.Migration, len(migrations))
    for i, m := range migrations {
        if m == nil {
            return nil, fmt.Errorf("nil migration encountered at index %d", i)
        }
        result[i] = *m
    }
    return result, nil
}

// LoadMigration loads a single migration via underlying loader
func (a *LoaderAdapter) LoadMigration(path string) (*types.Migration, error) {
    return a.loader.LoadMigration(path)
}

// ValidateFile validates a migration file by loading and delegating to loader's validators
func (a *LoaderAdapter) ValidateFile(path string) error {
    m, err := a.loader.LoadMigration(path)
    if err != nil {
        return err
    }
    return a.loader.ValidateMigration(m)
}
