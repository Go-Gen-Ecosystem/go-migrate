// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

// RunCallbacksRepairKey is the repair target for an interrupted run-level
// SQL callback sequence. It is reserved and cannot be a migration version.
const RunCallbacksRepairKey = "C:run-callbacks"

// runCallbackGuard records risk across the entire Up, including errors in
// subsequent migrations. It shares their session, but never their transaction.
type runCallbackGuard struct {
	a      *applier
	s      *snapshot
	active bool
}

func (g *runCallbackGuard) run(ctx context.Context, event string) error {
	if !slices.ContainsFunc(g.a.cat.callbacks[event], func(cb callback) bool { return len(cb.stmts) != 0 }) {
		return nil
	}
	if !g.active {
		// Read the actual next rank: a failed nontransactional migration can
		// have inserted a dirty row without advancing the run's snapshot.
		marker := history.Record{Version: RunCallbacksRepairKey, Kind: history.KindCallbacks,
			Description: "run-level SQL callbacks", Script: event, InstalledBy: clip(g.a.opts.installedBy, maxDescription)}
		var rank int64
		if err := g.a.callbackTransition(ctx, func(q history.Querier) error {
			var err error
			rank, err = g.a.store.Insert(ctx, q, marker)
			return err
		}); err != nil {
			return fmt.Errorf("record recovery marker before %s: %w", event, err)
		}
		g.active = true
		g.s.nextRank = rank + 1
	} else if err := g.a.checkCallbackSession(ctx); err != nil {
		// An error callback must not execute inside the failed migration's
		// abandoned transaction (or commit it as a side effect).
		return fmt.Errorf("before %s: %w", event, err)
	}
	if err := g.a.execCallbacks(ctx, g.a.conn, event, func() error {
		return g.a.lock.Fence(ctx, g.a.conn)
	}); err != nil {
		return err
	}
	if err := g.a.checkCallbackSession(ctx); err != nil {
		return fmt.Errorf("after %s: %w", event, err)
	}
	return nil
}

func (g *runCallbackGuard) finish(ctx context.Context) error {
	if !g.active {
		return nil
	}
	if err := g.a.callbackTransition(ctx, func(q history.Querier) error {
		return g.a.store.DeleteFailed(ctx, q, RunCallbacksRepairKey)
	}); err != nil {
		return fmt.Errorf("could not confirm callback completion; inspect status before retrying: %w", err)
	}
	return nil
}

func (g *runCallbackGuard) protect(err error) error {
	if err == nil || !g.active {
		return err
	}
	return fmt.Errorf("%w: %w", ErrCallbackRecoveryRequired, err)
}

// callbackTransition makes marker insertion and removal durable. On SQLite
// the lease fence and write commit together, and BEGIN rejects an open user
// transaction. On session-lock engines writes must run in autocommit mode.
func (a *applier) callbackTransition(ctx context.Context, change func(history.Querier) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.d.Name() != dialect.SQLite {
		if err := a.checkCallbackSession(ctx); err != nil {
			return err
		}
		return change(a.conn)
	}
	return a.fencedHistoryTransaction(ctx, func(tx *sql.Tx) error { return change(tx) })
}

func (a *applier) checkCallbackSession(ctx context.Context) error {
	switch a.d.Name() {
	case dialect.Postgres:
		return requirePostgresAutocommit(ctx, a.conn)
	case dialect.MySQL:
		return requireMySQLAutocommit(ctx, a.conn)
	case dialect.SQLServer:
		var transactions, implicit int
		if err := a.conn.QueryRowContext(ctx, "SELECT @@TRANCOUNT, @@OPTIONS & 2").Scan(&transactions, &implicit); err != nil {
			return fmt.Errorf("check SQL Server autocommit: %w", err)
		}
		if transactions != 0 || implicit != 0 {
			return errors.New("SQL Server session has an open or implicit transaction")
		}
	case dialect.SQLite:
		tx, err := a.conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("check SQLite autocommit: %w", err)
		}
		return tx.Rollback()
	case dialect.Oracle, dialect.DB2:
		// These drivers commit SQL outside database/sql transactions.
	}
	return nil
}

func requireMySQLAutocommit(ctx context.Context, q history.Querier) error {
	var auto bool
	if err := q.QueryRowContext(ctx, "SELECT @@session.autocommit").Scan(&auto); err != nil {
		return fmt.Errorf("check MySQL autocommit: %w", err)
	}
	if !auto {
		return errors.New("MySQL session has autocommit disabled")
	}
	// An explicit transaction is possible even when @@autocommit = 1.
	// A savepoint persists across statements only inside such a transaction.
	const probe = "gomigrate_internal_cb_tx_probe"
	if _, err := q.ExecContext(ctx, "SAVEPOINT "+probe); err != nil {
		return fmt.Errorf("check MySQL transaction state: %w", err)
	}
	_, err := q.ExecContext(ctx, "RELEASE SAVEPOINT "+probe)
	if err == nil {
		return errors.New("MySQL session is inside a transaction")
	}
	// MySQL error 1305 (42000) means the savepoint did not survive. Other
	// failures, including a lost connection, do not establish autocommit.
	// Match the driver's diagnostic without registering a driver here.
	if !strings.HasPrefix(err.Error(), "Error 1305 (42000): ") {
		return fmt.Errorf("check MySQL transaction state: %w", err)
	}
	return nil
}
