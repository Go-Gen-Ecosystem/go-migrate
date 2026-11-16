// Package history persists which migrations have been applied.
//
// The schema mirrors Flyway's: one row per applied version (repeatable
// migrations are keyed "R:<description>" and updated in place), ordered by
// installed_rank, with the checksum used for drift detection. All writes are
// expected to happen while the caller holds the migration lock, which is what
// makes the MAX(installed_rank)+1 assignment race-free.
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

// Kind classifies a history row.
type Kind string

// History row kinds.
const (
	KindSQL        Kind = "sql"
	KindGo         Kind = "go"
	KindRepeatable Kind = "repeatable"
	KindBaseline   Kind = "baseline"
)

// RepeatablePrefix prefixes the version key of repeatable migrations.
const RepeatablePrefix = "R:"

// ErrNotFound is returned when a version has no history row.
var ErrNotFound = errors.New("history row not found")

// Querier is satisfied by *sql.DB, *sql.Tx and *sql.Conn.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Record is one history row.
type Record struct {
	Rank        int64
	Version     string
	Description string
	Kind        Kind
	Script      string
	Checksum    string
	InstalledBy string
	InstalledOn time.Time
	ExecutionMS int64
	Success     bool
}

const columns = "installed_rank, version, description, kind, script, checksum, installed_by, installed_on, execution_ms, success"

// Store reads and writes one history table. It is immutable and safe for
// concurrent use.
type Store struct {
	d     *dialect.Dialect
	table dialect.Table
	q     string // quoted table name
}

// New returns a store for table t on dialect d.
func New(d *dialect.Dialect, t dialect.Table) *Store {
	return &Store{d: d, table: t, q: d.Quote(t)}
}

func (s *Store) ph(n int) string { return s.d.Placeholder(n) }

// Ensure creates the history table if it does not exist.
func (s *Store) Ensure(ctx context.Context, q Querier) error {
	if _, err := q.ExecContext(ctx, s.d.HistoryDDL(s.table)); err != nil {
		return fmt.Errorf("create history table %s: %w", s.table, err)
	}
	return nil
}

// Exists reports whether the history table exists, without creating it, so
// read-only operations never issue DDL.
func (s *Store) Exists(ctx context.Context, db *sql.DB) (bool, error) {
	return s.d.TableExists(ctx, db, s.table)
}

// List returns all rows in installed_rank order with a single query.
func (s *Store) List(ctx context.Context, q Querier) ([]Record, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+columns+" FROM "+s.q+" ORDER BY installed_rank")
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Record
	for rows.Next() {
		var r Record
		var kind string
		if err := rows.Scan(&r.Rank, &r.Version, &r.Description, &kind, &r.Script, &r.Checksum,
			&r.InstalledBy, &r.InstalledOn, &r.ExecutionMS, &r.Success); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		r.Kind = Kind(kind)
		r.InstalledOn = r.InstalledOn.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return out, nil
}

// Insert appends r with the next installed_rank and returns the rank used.
// InstalledOn defaults to now (UTC, microsecond precision).
func (s *Store) Insert(ctx context.Context, q Querier, r Record) (int64, error) {
	var rank int64
	if err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(installed_rank), 0) + 1 FROM "+s.q).Scan(&rank); err != nil {
		return 0, fmt.Errorf("next installed_rank: %w", err)
	}
	if r.InstalledOn.IsZero() {
		r.InstalledOn = Now()
	}
	_, err := q.ExecContext(ctx, "INSERT INTO "+s.q+" ("+columns+") VALUES ("+s.d.Placeholders(10)+")",
		rank, r.Version, r.Description, string(r.Kind), r.Script, r.Checksum,
		r.InstalledBy, r.InstalledOn, r.ExecutionMS, r.Success)
	if err != nil {
		return 0, fmt.Errorf("record version %s: %w", r.Version, err)
	}
	return rank, nil
}

// Complete marks version as successfully applied in executionMS.
func (s *Store) Complete(ctx context.Context, q Querier, version string, executionMS int64) error {
	return s.exec(ctx, q, "UPDATE "+s.q+" SET success = "+s.ph(1)+", execution_ms = "+s.ph(2)+" WHERE version = "+s.ph(3),
		true, executionMS, version)
}

// MarkDirty sets success = false on version before a non-transactional
// change starts, so a crash part-way is detectable on the next run.
func (s *Store) MarkDirty(ctx context.Context, q Querier, version string) error {
	return s.exec(ctx, q, "UPDATE "+s.q+" SET success = "+s.ph(1)+" WHERE version = "+s.ph(2), false, version)
}

// Reapply overwrites a repeatable migration's row after it ran again.
func (s *Store) Reapply(ctx context.Context, q Querier, r Record) error {
	if r.InstalledOn.IsZero() {
		r.InstalledOn = Now()
	}
	return s.exec(ctx, q, "UPDATE "+s.q+" SET description = "+s.ph(1)+", script = "+s.ph(2)+", checksum = "+s.ph(3)+
		", installed_by = "+s.ph(4)+", installed_on = "+s.ph(5)+", execution_ms = "+s.ph(6)+", success = "+s.ph(7)+
		" WHERE version = "+s.ph(8),
		r.Description, r.Script, r.Checksum, r.InstalledBy, r.InstalledOn, r.ExecutionMS, r.Success, r.Version)
}

// SetChecksum realigns the stored checksum of version (used by repair).
func (s *Store) SetChecksum(ctx context.Context, q Querier, version, checksum string) error {
	return s.exec(ctx, q, "UPDATE "+s.q+" SET checksum = "+s.ph(1)+" WHERE version = "+s.ph(2), checksum, version)
}

// Delete removes the row for version.
func (s *Store) Delete(ctx context.Context, q Querier, version string) error {
	return s.exec(ctx, q, "DELETE FROM "+s.q+" WHERE version = "+s.ph(1), version)
}

// DeleteFailed removes every row with success = false and returns how many.
func (s *Store) DeleteFailed(ctx context.Context, q Querier) (int64, error) {
	res, err := q.ExecContext(ctx, "DELETE FROM "+s.q+" WHERE success = "+s.ph(1), false)
	if err != nil {
		return 0, fmt.Errorf("delete failed history rows: %w", err)
	}
	return res.RowsAffected()
}

// exec runs a single-row write and reports ErrNotFound when no row matched.
func (s *Store) exec(ctx context.Context, q Querier, query string, args ...any) error {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update history: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update history: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Now returns the current time in the precision every dialect can store.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
