// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"
	"strings"
)

func db2Exists(table Table) (string, []any) {
	const query = `SELECT COUNT(*) FROM SYSCAT.TABLES
WHERE TABSCHEMA = COALESCE(CAST(? AS VARCHAR(128)), RTRIM(CURRENT SCHEMA)) AND TABNAME = ? AND TYPE = 'T'`
	var schema any
	if table.Schema != "" {
		schema = table.Schema
	}
	return query, []any{schema, strings.ToUpper(table.Name)}
}

func (*Dialect) db2Lock(ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
	// Db2 allows user-defined IDs from 0 through 2^30−1.
	digest := sha256.Sum256([]byte(LockKey(table)))
	lockID := binary.BigEndian.Uint32(digest[:4]) & ((1 << 30) - 1)
	id := fmt.Sprintf("DECIMAL(%d, 10)", lockID)
	request := db2LockBlock("REQUEST("+id+", SYSIBMADM.DBMS_LOCK.X_MODE, 0, FALSE)",
		"IF lock_status = 1 THEN SIGNAL SQLSTATE '75001'; END IF;")
	release := db2LockBlock("RELEASE("+id+")", "")
	return sessionLock(ctx, conn, once,
		func(ctx context.Context) (bool, error) {
			_, err := conn.ExecContext(ctx, request)
			// The driver exposes SQLSTATE as a struct field. Match its exact
			// diagnostic without importing a driver into the library.
			const busy = "db2: SQLCODE=-438, SQLSTATE=75001"
			if err != nil && (err.Error() == busy || strings.HasPrefix(err.Error(), busy+": ")) {
				return false, nil
			}
			return err == nil, err
		},
		func(ctx context.Context) error {
			_, err := conn.ExecContext(ctx, release)
			return err
		})
}

// Db2 permits these functions only in compiled SQL PL assignments. SIGNAL
// carries the outcome back without a persistent procedure or a second session.
func db2LockBlock(call, contention string) string {
	return `BEGIN
DECLARE lock_status INTEGER;
SET lock_status = SYSIBMADM.DBMS_LOCK.` + call + `;
` + contention + `
IF lock_status <> 0 THEN SIGNAL SQLSTATE '75002'; END IF;
END`
}
