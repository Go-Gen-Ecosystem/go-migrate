// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrLockTimeout is returned when the migration lock is still held by
// another process after the configured wait.
var ErrLockTimeout = errors.New("timed out waiting for the migration lock")

// ErrLockLost is returned when a lease-based lock expired and was taken over
// while held. The change that detected it is rolled back.
var ErrLockLost = errors.New("migration lock was lost while held")

// Polling parameters.
const (
	pollMin      = 50 * time.Millisecond
	pollMax      = time.Second
	mysqlMaxLock = 64

	lockCleanupTimeout = 30 * time.Second
)

// leaseTTL is how long an SQLite lease survives without renewal; a variable
// so tests can shorten it. Every migration renews it (see Lock.Fence).
var leaseTTL = 2 * time.Minute

// Querier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// locker acquires the lock on an already pinned connection.
type locker func(d *Dialect, ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error)

// Lock is a held migration lock.
//
// All work under the lock must run on Conn. For session locks (PostgreSQL,
// MySQL) that means losing the server session fails the work instead of
// letting it continue unprotected, and a pool limited to one connection
// cannot deadlock.
type Lock struct {
	Conn       *sql.Conn
	Table      Table
	fence      func(ctx context.Context, q Querier) error
	unlock     func(ctx context.Context) error
	afterClose func() error
	// discard closes the connection on release instead of pooling it.
	discard bool
	once    sync.Once
}

// Fence verifies that the lock is still held and, for leases, renews it.
// Call it inside each migration's transaction so ownership and the change
// commit atomically; a lost lease makes the transaction roll back.
func (l *Lock) Fence(ctx context.Context, q Querier) error {
	if l.fence == nil {
		return nil
	}
	return l.fence(ctx, q)
}

// Release unlocks and frees the pinned connection. For session locks the
// connection is discarded rather than pooled, so session state set by
// migrations (SET ..., USE ...) never leaks into the pool and a connection
// whose unlock failed can never be reused while still holding the lock.
// SQLite connections are pooled normally (an in-memory database lives only
// as long as its connection). Cleanup has its own deadline, and Release is
// safe to call more than once.
func (l *Lock) Release(ctx context.Context) error {
	var err error
	l.once.Do(func() {
		if l.afterClose != nil {
			defer func() { err = errors.Join(err, l.afterClose()) }()
		}
		cleanup, cancel := lockCleanupContext(ctx)
		defer cancel()
		err = l.unlock(cleanup)
		if l.discard {
			discardSession(l.Conn)
			return
		}
		if cerr := l.Conn.Close(); err == nil {
			err = cerr
		}
	})
	return err
}

// Lock pins one session, resolves its history table, and acquires the lock
// within the same budget. A zero wait makes one attempt under the caller's
// context. The acquired lock owns the connection until Release.
func (d *Dialect) Lock(ctx context.Context, db *sql.DB, table Table, wait time.Duration) (*Lock, error) {
	if wait <= 0 {
		return d.acquireLock(ctx, db, table, true)
	}
	acquire, cancel := context.WithTimeoutCause(ctx, wait, ErrLockTimeout)
	defer cancel()
	held, err := d.acquireLock(acquire, db, table, false)
	if err != nil && errors.Is(context.Cause(acquire), ErrLockTimeout) {
		return nil, fmt.Errorf("%w: %w", ErrLockTimeout, err)
	}
	return held, err
}

func (d *Dialect) acquireLock(ctx context.Context, db *sql.DB, table Table, once bool) (held *Lock, err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve lock connection: %w", err)
	}
	defer func() {
		if held == nil {
			_ = conn.Close()
		}
	}()
	table, err = d.ResolveTable(ctx, conn, table)
	if err != nil {
		return nil, err
	}
	held, err = d.locker(d, ctx, conn, table, once)
	if err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	held.Conn, held.Table = conn, table
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, held.Release(ctx))
	}
	return held, nil
}

func lockCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), lockCleanupTimeout)
}

func discardSession(conn *sql.Conn) {
	// Returning ErrBadConn from Raw makes database/sql close the session.
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

// tryFunc makes one non-blocking acquisition attempt.
type tryFunc func(ctx context.Context) (bool, error)

// poll shares the acquisition deadline with SQL and connection checkout.
func poll(ctx context.Context, once bool, try tryFunc) error {
	for delay := pollMin; ; delay = min(delay*2, pollMax) {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := try(ctx)
		if err != nil || ok {
			return err
		}
		if once {
			return ErrLockTimeout
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// postgresLock uses a session-level advisory lock. PostgreSQL releases it if
// the session dies.
func (*Dialect) postgresLock(ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
	id := advisoryID(LockKey(table))
	return sessionLock(ctx, conn, once,
		func(ctx context.Context) (bool, error) {
			var ok bool
			err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", id).Scan(&ok)
			return ok, err
		},
		func(ctx context.Context) error {
			_, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", id)
			return err
		})
}

// mysqlLock uses GET_LOCK. MySQL releases it if the session dies.
func (*Dialect) mysqlLock(ctx context.Context, conn *sql.Conn, table Table, once bool) (*Lock, error) {
	name := LockKey(table)
	if len(name) > mysqlMaxLock {
		name = fmt.Sprintf("go-migrate:%016x", advisoryID(name))
	}
	return sessionLock(ctx, conn, once,
		func(ctx context.Context) (bool, error) {
			var got sql.NullInt64
			if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&got); err != nil {
				return false, err
			}
			return got.Valid && got.Int64 == 1, nil
		},
		func(ctx context.Context) error {
			_, err := conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", name)
			return err
		})
}

func sessionLock(ctx context.Context, conn *sql.Conn, once bool, try tryFunc, unlock func(context.Context) error) (*Lock, error) {
	if err := poll(ctx, once, try); err != nil {
		// The server may have acquired the lock before a response was lost.
		discardSession(conn)
		return nil, err
	}
	return &Lock{discard: true, unlock: unlock}, nil
}

// sqliteOwners holds the owners of all live SQLite runs in this process.
// An in-memory database can be shared by multiple pools, all of which see
// the same lease owner even when they use different attachment names.
var sqliteOwners sync.Map

// sqliteLock holds a file guard for the lifetime of a file-backed run. The
// row lease is still fenced and remains recoverable after its expiry.
func (d *Dialect) sqliteLock(ctx context.Context, conn *sql.Conn, table Table, once bool) (held *Lock, err error) {
	closeGuard, err := sqliteFileGuard(ctx, conn, table, once)
	if err != nil {
		return nil, err
	}
	owner := ownerID()
	sqliteOwners.Store(owner, struct{}{})
	closeLock := func() error {
		sqliteOwners.Delete(owner)
		return closeGuard()
	}
	defer func() {
		if held == nil {
			err = errors.Join(err, closeLock())
		}
	}()

	table.Name += "_lock"
	tbl := d.Quote(table)
	ddl := "CREATE TABLE IF NOT EXISTS " + tbl + " (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, expires_at INTEGER NOT NULL)"
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		return nil, fmt.Errorf("create lock table: %w", err)
	}
	clear := func(ctx context.Context, q Querier) (int64, error) {
		res, err := q.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE id = 1 AND owner = ?", owner)
		if err != nil {
			return 0, fmt.Errorf("delete migration lease: %w", err)
		}
		return res.RowsAffected()
	}
	uncertain := false
	err = poll(ctx, once, func(ctx context.Context) (bool, error) {
		now := time.Now().UnixMilli()
		if err := sqliteClearExpired(ctx, conn, tbl, now); err != nil {
			return false, transient(err)
		}
		res, err := conn.ExecContext(ctx, "INSERT INTO "+tbl+" (id, owner, expires_at) VALUES (1, ?, ?) ON CONFLICT(id) DO NOTHING",
			owner, now+leaseTTL.Milliseconds())
		if err != nil {
			err = transient(err)
			uncertain = err != nil
			return false, err
		}
		n, err := res.RowsAffected()
		uncertain = err != nil
		return n == 1, err
	})
	// An interrupted INSERT can have committed before the driver reported
	// failure. Remove only this attempt's owner row.
	if uncertain {
		cleanup, cancel := lockCleanupContext(ctx)
		defer cancel()
		_, clearErr := clear(cleanup, conn)
		err = errors.Join(err, clearErr)
	}
	if err != nil {
		return nil, err
	}
	return &Lock{
		afterClose: closeLock,
		fence: func(ctx context.Context, q Querier) error {
			res, err := q.ExecContext(ctx, "UPDATE "+tbl+" SET expires_at = ? WHERE id = 1 AND owner = ?",
				time.Now().Add(leaseTTL).UnixMilli(), owner)
			if err != nil {
				return fmt.Errorf("renew migration lock: %w", err)
			}
			if n, err := res.RowsAffected(); err != nil || n != 1 {
				return ErrLockLost
			}
			return nil
		},
		unlock: func(ctx context.Context) error {
			// A failed NoTx script can leave a transaction open. ROLLBACK
			// also errors in autocommit mode. A new transaction proves that
			// the lease cleanup cannot run within an abandoned transaction.
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				discardSession(conn)
				return fmt.Errorf("begin SQLite lease cleanup: %w", err)
			}
			defer func() { _ = tx.Rollback() }()
			n, err := clear(ctx, tx)
			if err != nil {
				return err
			}
			if n != 1 {
				return ErrLockLost
			}
			return tx.Commit()
		},
	}, nil
}

func sqliteClearExpired(ctx context.Context, conn *sql.Conn, table string, now int64) error {
	var owner string
	err := conn.QueryRowContext(ctx, "SELECT owner FROM "+table+" WHERE id = 1 AND expires_at < ?", now).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, live := sqliteOwners.Load(owner); live {
		return nil
	}
	// Recheck identity and expiry after the read so a newer holder or
	// a renewal between statements cannot be removed.
	_, err = conn.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = 1 AND owner = ? AND expires_at < ?", owner, now)
	return err
}

// transient maps "database is busy/locked" to "not acquired yet", so a
// waiter keeps polling instead of failing while another process writes.
func transient(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "SQLITE_LOCKED") || strings.Contains(msg, "database is locked") {
		return nil
	}
	return err
}

// advisoryID hashes key into the 64-bit space of PostgreSQL advisory locks.
func advisoryID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64()) //nolint:gosec // intentional wrap into the signed advisory-lock key space
}

// ownerID identifies a lock holder: host and pid for operators, plus random
// bytes so two holders never share an identity (e.g. pid 1 in containers).
func ownerID() string {
	host, _ := os.Hostname()
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), hex.EncodeToString(b))
}

// LockKey returns the lock key for a (schema-qualified) history table, so
// applications sharing one server but using different tables never contend.
func LockKey(t Table) string {
	return "go-migrate:" + t.String()
}
