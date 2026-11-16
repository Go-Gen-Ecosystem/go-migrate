// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
)

// sqliteFileGuard keeps a contender out for the entire run, independently of
// transactions on the migration connection. The OS releases it on exit. The
// file stays in place so a waiter can never be left locking an old inode.
func sqliteFileGuard(ctx context.Context, conn *sql.Conn, table Table, once bool) (func() error, error) {
	path, err := sqliteDatabaseFile(ctx, conn, table.Schema)
	if err != nil {
		return nil, err
	}
	if path == "" {
		// In-memory and temporary databases cannot be shared across processes.
		// Their live owners are protected by the local lease registry.
		return func() error { return nil }, nil
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite database file: %w", err)
	}
	guard := flock.New(path+".gomigrate-"+strings.ToLower(table.Name)+".lock", flock.SetPermissions(0o660))
	if err := poll(ctx, once, func(context.Context) (bool, error) { return guard.TryLock() }); err != nil {
		return nil, fmt.Errorf("acquire SQLite file guard: %w", err)
	}
	return guard.Close, nil
}

func sqliteDatabaseFile(ctx context.Context, conn *sql.Conn, schema string) (string, error) {
	if strings.EqualFold(schema, "temp") {
		return "", nil
	}
	var path string
	if err := conn.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name COLLATE NOCASE = ?", schema).Scan(&path); err != nil {
		return "", fmt.Errorf("find SQLite database file for %s: %w", schema, err)
	}
	return path, nil
}
