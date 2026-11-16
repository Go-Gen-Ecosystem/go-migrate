// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

func oracleExists(table Table) (string, []any) {
	const query = `SELECT COUNT(*) FROM ALL_TABLES
WHERE OWNER = COALESCE(:1, SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA')) AND TABLE_NAME = :2`
	return query, []any{table.Schema, strings.ToUpper(table.Name)}
}

func (*Dialect) oracleLock(ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
	const allocate = `BEGIN SYS.DBMS_LOCK.ALLOCATE_UNIQUE_AUTONOMOUS(:resource, :handle); END;`
	const request = `BEGIN :result := SYS.DBMS_LOCK.REQUEST(:handle, SYS.DBMS_LOCK.X_MODE, 0, FALSE); END;`
	const release = `BEGIN :result := SYS.DBMS_LOCK.RELEASE(:handle); END;`
	resource := fmt.Sprintf("go-migrate:%x", sha256.Sum256([]byte(LockKey(table))))
	var handle string
	if _, err := conn.ExecContext(ctx, allocate, sql.Named("resource", resource), sql.Named("handle", sql.Out{Dest: &handle})); err != nil {
		discardSession(conn)
		return nil, fmt.Errorf("allocate Oracle migration lock: %w", err)
	}
	call := func(ctx context.Context, sqlBlock string) (int64, error) {
		var result int64
		_, err := conn.ExecContext(ctx, sqlBlock, sql.Named("result", sql.Out{Dest: &result}), sql.Named("handle", handle))
		return result, err
	}
	return sessionLock(ctx, conn, once,
		func(ctx context.Context) (bool, error) {
			result, err := call(ctx, request)
			if err != nil {
				return false, err
			}
			switch result {
			case 0:
				return true, nil
			case 1:
				return false, nil
			default:
				return false, fmt.Errorf("request Oracle DBMS_LOCK: returned %d", result)
			}
		},
		func(ctx context.Context) error {
			result, err := call(ctx, release)
			if err != nil {
				return err
			}
			if result != 0 {
				return fmt.Errorf("release Oracle DBMS_LOCK: returned %d", result)
			}
			return nil
		})
}
