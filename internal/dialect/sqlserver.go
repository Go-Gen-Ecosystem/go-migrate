// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Application lock names are compared as binary strings even when the catalog
// is case-insensitive. One resource per database covers all spellings of the
// same history table, including before that table exists.
const sqlServerLockResource = "go-migrate:history"

func sqlServerQuoteIdentifier(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func resolveSQLServerTable(ctx context.Context, q Querier, table Table) (Table, error) {
	var schema any
	if table.Schema != "" {
		schema = table.Schema
	}
	if err := q.QueryRowContext(ctx, "SELECT DB_NAME(), COALESCE(@p1, SCHEMA_NAME())", schema).
		Scan(&table.Catalog, &table.Schema); err != nil {
		return Table{}, fmt.Errorf("resolve SQL Server history table: %w", err)
	}
	return table, nil
}

func sqlServerHistoryGuard(quoted string) string {
	return "IF OBJECT_ID(N'" + strings.ReplaceAll(quoted, "'", "''") + "', N'U') IS NULL\nCREATE TABLE "
}

func sqlServerExists(table Table) (string, []any) {
	var schema any
	if table.Schema != "" {
		schema = table.Schema
	}
	return sqlServerTables(table.Catalog) + " AND t.name = @p2", []any{schema, table.Name}
}

func sqlServerTables(catalog string) string {
	owner := "sys."
	if catalog != "" {
		owner = sqlServerQuoteIdentifier(catalog) + "." + owner
	}
	return "SELECT COUNT(*) FROM " + owner + "tables AS t JOIN " + owner + "schemas AS s ON s.schema_id = t.schema_id" +
		" WHERE t.is_ms_shipped = 0 AND s.name = COALESCE(@p1, SCHEMA_NAME())"
}

func (d *Dialect) countSQLServerTables(ctx context.Context, q Querier, schema string, exclude []string) (int, error) {
	query := sqlServerTables("")
	args := make([]any, 1, len(exclude)+1)
	args[0] = schema
	for _, name := range exclude {
		args = append(args, name)
		// Compare identifiers under the catalog's collation.
		query += " AND t.name <> " + d.Placeholder(len(args))
	}
	var n int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count tables in %s: %w", schema, err)
	}
	return n, nil
}

func (d *Dialect) sqlServerLock(ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
	const acquire = `DECLARE @result int;
EXEC @result = sys.sp_getapplock @Resource = @p1, @LockMode = 'Exclusive', @LockOwner = 'Session', @LockTimeout = 0;
SELECT @result;`
	const release = `DECLARE @result int;
EXEC @result = sys.sp_releaseapplock @Resource = @p1, @LockOwner = 'Session';
SELECT @result;`
	return sessionLock(ctx, conn, once,
		func(ctx context.Context) (bool, error) {
			var result int
			if err := conn.QueryRowContext(ctx, acquire, sqlServerLockResource).Scan(&result); err != nil {
				return false, err
			}
			switch result {
			case 0, 1:
				return true, nil
			case -1:
				return false, nil
			case -2:
				return false, context.Canceled
			case -3:
				return false, errors.New("SQL Server application lock deadlock")
			default:
				return false, fmt.Errorf("sp_getapplock returned %d", result)
			}
		},
		func(ctx context.Context) error {
			// A migration may have switched the session's database using USE.
			if _, err := conn.ExecContext(ctx, "USE "+d.quoteIdentifier(table.Catalog)); err != nil {
				return fmt.Errorf("restore SQL Server lock database: %w", err)
			}
			var result int
			if err := conn.QueryRowContext(ctx, release, sqlServerLockResource).Scan(&result); err != nil {
				return err
			}
			if result != 0 {
				return fmt.Errorf("sp_releaseapplock returned %d", result)
			}
			return nil
		})
}
