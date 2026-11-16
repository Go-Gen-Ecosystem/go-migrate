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

// Locker acquires the cross-process migration lock for key.
type Locker interface {
	Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (*Lock, error)
}

// Lock is a held migration lock.
//
// All work under the lock must run on Conn. For session locks (PostgreSQL,
// MySQL) that means losing the server session fails the work instead of
// letting it continue unprotected, and a pool limited to one connection
// cannot deadlock.
type Lock struct {
	Conn   *sql.Conn
	fence  func(ctx context.Context, q Querier) error
	unlock func(ctx context.Context) error
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
// as long as its connection). Release is safe to call more than once.
func (l *Lock) Release(ctx context.Context) error {
	var err error
	l.once.Do(func() {
		err = l.unlock(ctx)
		if l.discard {
			// Returning ErrBadConn from Raw makes database/sql close the
			// connection instead of returning it to the pool.
			_ = l.Conn.Raw(func(any) error { return driver.ErrBadConn })
			return
		}
		if cerr := l.Conn.Close(); err == nil {
			err = cerr
		}
	})
	return err
}

// tryFunc makes one non-blocking acquisition attempt.
type tryFunc func(ctx context.Context) (bool, error)

// poll calls try with capped exponential backoff until it succeeds, wait
// elapses (ErrLockTimeout) or ctx is done.
func poll(ctx context.Context, wait time.Duration, try tryFunc) error {
	deadline := time.Now().Add(wait)
	delay := pollMin
	for {
		ok, err := try(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if !time.Now().Before(deadline) {
			return ErrLockTimeout
		}
		t := time.NewTimer(min(delay, time.Until(deadline)))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		delay = min(delay*2, pollMax)
	}
}

// pgLocker uses a session-level advisory lock. PostgreSQL releases it if
// the session dies.
type pgLocker struct{}

func (pgLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (*Lock, error) {
	id := advisoryID(key)
	return sessionLock(ctx, db, wait,
		func(ctx context.Context, c *sql.Conn) (bool, error) {
			var ok bool
			err := c.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", id).Scan(&ok)
			return ok, err
		},
		func(ctx context.Context, c *sql.Conn) error {
			_, err := c.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", id)
			return err
		})
}

// mysqlLocker uses GET_LOCK; MySQL releases it if the session dies.
type mysqlLocker struct{}

func (mysqlLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (*Lock, error) {
	name := key
	if len(name) > mysqlMaxLock {
		name = fmt.Sprintf("go-migrate:%016x", advisoryID(key))
	}
	return sessionLock(ctx, db, wait,
		func(ctx context.Context, c *sql.Conn) (bool, error) {
			var got sql.NullInt64
			if err := c.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&got); err != nil {
				return false, err
			}
			return got.Valid && got.Int64 == 1, nil
		},
		func(ctx context.Context, c *sql.Conn) error {
			_, err := c.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", name)
			return err
		})
}

// sessionLock takes a session-scoped lock on a dedicated connection, which
// then becomes the connection all migration work runs on.
func sessionLock(ctx context.Context, db *sql.DB, wait time.Duration,
	try func(context.Context, *sql.Conn) (bool, error),
	unlock func(context.Context, *sql.Conn) error,
) (*Lock, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve lock connection: %w", err)
	}
	if err := poll(ctx, wait, func(ctx context.Context) (bool, error) { return try(ctx, conn) }); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	return &Lock{Conn: conn, discard: true, unlock: func(ctx context.Context) error { return unlock(ctx, conn) }}, nil
}

// sqliteLocker uses a single-row lease table. There is no heartbeat
// goroutine: each migration renews the lease inside its own transaction
// (Fence), which SQLite's single-writer model makes atomic with the change.
// A crashed holder's lease expires after leaseTTL.
type sqliteLocker struct{}

func (sqliteLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (*Lock, error) {
	t, err := leaseTable(key)
	if err != nil {
		return nil, err
	}
	tbl := dialects[SQLite].Quote(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve lock connection: %w", err)
	}
	ddl := "CREATE TABLE IF NOT EXISTS " + tbl + " (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, expires_at INTEGER NOT NULL)"
	if _, err := conn.ExecContext(ctx, ddl); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("create lock table: %w", err)
	}
	owner := ownerID()
	err = poll(ctx, wait, func(ctx context.Context) (bool, error) {
		now := time.Now().UnixMilli()
		if _, err := conn.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE expires_at < ?", now); err != nil {
			return false, transient(err)
		}
		res, err := conn.ExecContext(ctx, "INSERT INTO "+tbl+" (id, owner, expires_at) VALUES (1, ?, ?) ON CONFLICT(id) DO NOTHING",
			owner, now+leaseTTL.Milliseconds())
		if err != nil {
			return false, transient(err)
		}
		n, err := res.RowsAffected()
		return n == 1, err
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	return &Lock{
		Conn: conn,
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
			res, err := conn.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE id = 1 AND owner = ?", owner)
			if err != nil {
				return fmt.Errorf("release migration lock: %w", err)
			}
			if n, _ := res.RowsAffected(); n != 1 {
				return ErrLockLost
			}
			return nil
		},
	}, nil
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

// leaseTable derives the SQLite lease table from the lock key, keeping the
// history table's schema so separately attached databases never share one.
func leaseTable(key string) (Table, error) {
	t, err := ParseTable(strings.TrimPrefix(key, "go-migrate:"))
	if err != nil {
		return Table{}, err
	}
	t.Name += "_lock"
	return t, nil
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
