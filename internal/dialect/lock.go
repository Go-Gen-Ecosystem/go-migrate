package dialect

import (
	"context"
	"crypto/rand"
	"database/sql"
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

// ErrLockLost is returned by release when a lease-based lock expired or was
// taken over while held.
var ErrLockLost = errors.New("migration lock was lost while held")

// Polling parameters.
const (
	pollMin      = 50 * time.Millisecond
	pollMax      = time.Second
	mysqlMaxLock = 64
)

// Lease parameters for the SQLite lock; variables so tests can shorten them.
var (
	leaseTTL       = 30 * time.Second
	leaseHeartbeat = 10 * time.Second
)

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

// pgLocker uses a session-level advisory lock held on a dedicated
// connection. PostgreSQL releases it automatically if the session dies.
type pgLocker struct{}

func (pgLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (context.Context, func(context.Context) error, error) {
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

// mysqlLocker uses GET_LOCK on a dedicated connection; MySQL releases it
// automatically if the session dies.
type mysqlLocker struct{}

func (mysqlLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (context.Context, func(context.Context) error, error) {
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

// sessionLock pins one pooled connection for the lifetime of the lock, since
// session-scoped locks belong to the connection that took them.
func sessionLock(ctx context.Context, db *sql.DB, wait time.Duration,
	try func(context.Context, *sql.Conn) (bool, error),
	unlock func(context.Context, *sql.Conn) error,
) (context.Context, func(context.Context) error, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("reserve lock connection: %w", err)
	}
	if err := poll(ctx, wait, func(ctx context.Context) (bool, error) { return try(ctx, conn) }); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	var once sync.Once
	release := func(ctx context.Context) error {
		var err error
		once.Do(func() {
			err = unlock(ctx, conn)
			if cerr := conn.Close(); err == nil {
				err = cerr
			}
		})
		return err
	}
	return ctx, release, nil
}

// sqliteLocker uses a single-row lease table. A heartbeat goroutine extends
// the lease while held; if renewal fails the returned context is cancelled
// so in-flight work stops instead of continuing unprotected. A crashed
// holder's lease expires after leaseTTL.
type sqliteLocker struct{}

func (sqliteLocker) Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (context.Context, func(context.Context) error, error) {
	t, err := ParseTable(leaseTableName(key))
	if err != nil {
		return nil, nil, err
	}
	tbl := dialects[SQLite].Quote(t)
	ddl := "CREATE TABLE IF NOT EXISTS " + tbl + " (id INTEGER PRIMARY KEY CHECK (id = 1), owner TEXT NOT NULL, expires_at INTEGER NOT NULL)"
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return nil, nil, fmt.Errorf("create lock table: %w", err)
	}
	owner := ownerID()
	err = poll(ctx, wait, func(ctx context.Context) (bool, error) {
		now := time.Now().UnixMilli()
		if _, err := db.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE expires_at < ?", now); err != nil {
			return false, fmt.Errorf("expire stale lock: %w", err)
		}
		res, err := db.ExecContext(ctx, "INSERT INTO "+tbl+" (id, owner, expires_at) VALUES (1, ?, ?) ON CONFLICT(id) DO NOTHING",
			owner, now+leaseTTL.Milliseconds())
		if err != nil {
			return false, fmt.Errorf("insert lock row: %w", err)
		}
		n, err := res.RowsAffected()
		return n == 1, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("acquire migration lock: %w", err)
	}

	lockCtx, cancel := context.WithCancelCause(ctx)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(leaseHeartbeat)
		defer tick.Stop()
		renewed := time.Now()
		for {
			select {
			case <-stop:
				return
			case <-lockCtx.Done():
				return
			case <-tick.C:
				res, err := db.ExecContext(lockCtx, "UPDATE "+tbl+" SET expires_at = ? WHERE id = 1 AND owner = ?",
					time.Now().Add(leaseTTL).UnixMilli(), owner)
				if err != nil {
					// A busy database (e.g. our own long migration transaction
					// holding SQLite's single writer slot) is transient: keep
					// trying until the lease has genuinely expired.
					if time.Since(renewed) >= leaseTTL {
						cancel(fmt.Errorf("%w: %w", ErrLockLost, err))
						return
					}
					continue
				}
				if n, err := res.RowsAffected(); err != nil || n != 1 {
					cancel(ErrLockLost) // another process owns the lease now
					return
				}
				renewed = time.Now()
			}
		}
	}()

	var once sync.Once
	release := func(ctx context.Context) error {
		var err error
		once.Do(func() {
			close(stop)
			wg.Wait() // the heartbeat never outlives the lock
			lost := context.Cause(lockCtx)
			cancel(nil)
			res, derr := db.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE id = 1 AND owner = ?", owner)
			switch {
			case lost != nil && !errors.Is(lost, context.Canceled):
				err = lost
			case derr != nil:
				err = fmt.Errorf("release migration lock: %w", derr)
			default:
				if n, _ := res.RowsAffected(); n != 1 {
					err = ErrLockLost
				}
			}
		})
		return err
	}
	return lockCtx, release, nil
}

// leaseTableName derives the SQLite lease table from the history table key.
func leaseTableName(key string) string {
	name := strings.TrimPrefix(key, "go-migrate:")
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return name + "_lock"
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

// LockKey returns the lock key for a history table, so that applications
// sharing one database but using different history tables never contend.
func LockKey(t Table) string {
	return "go-migrate:" + t.String()
}
