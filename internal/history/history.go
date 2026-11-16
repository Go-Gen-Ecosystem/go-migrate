// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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
	// KindMarked is a migration recorded without running (precondition
	// onFail=MARK_RAN): rolling it back only removes the row.
	KindMarked Kind = "marked"
)

// RepeatablePrefix prefixes the version key of repeatable migrations.
const RepeatablePrefix = "R:"

// ErrNotFound is returned when a version has no history row.
var ErrNotFound = errors.New("history row not found")

// Querier is satisfied by *sql.DB, *sql.Tx and *sql.Conn.
type Querier = dialect.Querier

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
	// Tag is a release label set by Store.Tag (Liquibase tags); "" if none.
	Tag string
}

const columns = "installed_rank, version, description, kind, script, checksum, installed_by, installed_on, execution_ms, success"

// Store reads and writes one history table. It is immutable and safe for
// concurrent use. Every statement is built once, in New, so the per-migration
// path does no string assembly.
type Store struct {
	d     *dialect.Dialect
	table dialect.Table
	sql   queries
}

type queries struct {
	list, nextRank, insert, complete, markDirty, reapply, realign, delete, deleteFailed, clearTag, setTag string
}

// New returns a store for table t on dialect d.
func New(d *dialect.Dialect, t dialect.Table) *Store {
	q, p := d.Quote(t), d.Placeholder
	return &Store{d: d, table: t, sql: queries{
		list:      "SELECT " + columns + ", tag FROM " + q + " ORDER BY installed_rank",
		nextRank:  "SELECT COALESCE(MAX(installed_rank), 0) + 1 FROM " + q,
		insert:    "INSERT INTO " + q + " (" + columns + ") VALUES (" + d.Placeholders(10) + ")",
		complete:  "UPDATE " + q + " SET success = " + p(1) + ", execution_ms = " + p(2) + " WHERE version = " + p(3),
		markDirty: "UPDATE " + q + " SET success = " + p(1) + " WHERE version = " + p(2),
		reapply: "UPDATE " + q + " SET description = " + p(1) + ", script = " + p(2) + ", checksum = " + p(3) +
			", installed_by = " + p(4) + ", installed_on = " + p(5) + ", execution_ms = " + p(6) + ", success = " + p(7) +
			" WHERE version = " + p(8),
		realign:      "UPDATE " + q + " SET checksum = " + p(1) + ", kind = " + p(2) + " WHERE version = " + p(3),
		delete:       "DELETE FROM " + q + " WHERE version = " + p(1),
		deleteFailed: "DELETE FROM " + q + " WHERE success = " + p(1),
		clearTag:     "UPDATE " + q + " SET tag = NULL WHERE tag = " + p(1),
		setTag:       "UPDATE " + q + " SET tag = " + p(1) + " WHERE version = " + p(2),
	}}
}

// Ensure creates the history table if it does not exist.
func (s *Store) Ensure(ctx context.Context, q Querier) error {
	if _, err := q.ExecContext(ctx, s.d.HistoryDDL(s.table)); err != nil {
		return fmt.Errorf("create history table %s: %w", s.table, err)
	}
	return nil
}

// Exists reports whether the history table exists, without creating it, so
// read-only operations never issue DDL.
func (s *Store) Exists(ctx context.Context, q Querier) (bool, error) {
	return s.d.TableExists(ctx, q, s.table)
}

// List returns all rows in installed_rank order with a single query.
func (s *Store) List(ctx context.Context, q Querier) ([]Record, error) {
	rows, err := q.QueryContext(ctx, s.sql.list)
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Record
	for rows.Next() {
		var r Record
		var kind string
		var tag sql.NullString
		if err := rows.Scan(&r.Rank, &r.Version, &r.Description, &kind, &r.Script, &r.Checksum,
			&r.InstalledBy, timestamp{&r.InstalledOn}, &r.ExecutionMS, &r.Success, &tag); err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}
		r.Kind = Kind(kind)
		r.Tag = tag.String
		r.InstalledOn = r.InstalledOn.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return out, nil
}

// Insert appends r and returns the installed_rank used. A zero r.Rank means
// "next": it is read with MAX()+1. Callers that hold the migration lock and
// already know the next rank pass it to save a round-trip per migration.
// InstalledOn defaults to now (UTC, microsecond precision).
func (s *Store) Insert(ctx context.Context, q Querier, r Record) (int64, error) {
	rank := r.Rank
	if rank <= 0 {
		if err := q.QueryRowContext(ctx, s.sql.nextRank).Scan(&rank); err != nil {
			return 0, fmt.Errorf("next installed_rank: %w", err)
		}
	}
	if r.InstalledOn.IsZero() {
		r.InstalledOn = Now()
	}
	_, err := q.ExecContext(ctx, s.sql.insert,
		rank, r.Version, r.Description, string(r.Kind), r.Script, r.Checksum,
		r.InstalledBy, r.InstalledOn, r.ExecutionMS, r.Success)
	if err != nil {
		return 0, fmt.Errorf("record version %s: %w", r.Version, err)
	}
	return rank, nil
}

// Complete marks version as successfully applied in executionMS.
func (s *Store) Complete(ctx context.Context, q Querier, version string, executionMS int64) error {
	return s.exec(ctx, q, s.sql.complete, true, executionMS, version)
}

// MarkDirty sets success = false on version before a non-transactional
// change starts, so a crash part-way is detectable on the next run.
func (s *Store) MarkDirty(ctx context.Context, q Querier, version string) error {
	return s.exec(ctx, q, s.sql.markDirty, false, version)
}

// Reapply overwrites a repeatable migration's row after it ran again.
func (s *Store) Reapply(ctx context.Context, q Querier, r Record) error {
	if r.InstalledOn.IsZero() {
		r.InstalledOn = Now()
	}
	return s.exec(ctx, q, s.sql.reapply,
		r.Description, r.Script, r.Checksum, r.InstalledBy, r.InstalledOn, r.ExecutionMS, r.Success, r.Version)
}

// Realign records version's current checksum and kind (used by repair to
// accept a deliberately edited or re-implemented migration).
func (s *Store) Realign(ctx context.Context, q Querier, version, checksum string, kind Kind) error {
	return s.exec(ctx, q, s.sql.realign, checksum, string(kind), version)
}

// Delete removes the row for version.
func (s *Store) Delete(ctx context.Context, q Querier, version string) error {
	return s.exec(ctx, q, s.sql.delete, version)
}

// DeleteFailed removes every row with success = false and returns how many.
func (s *Store) DeleteFailed(ctx context.Context, q Querier) (int64, error) {
	res, err := q.ExecContext(ctx, s.sql.deleteFailed, false)
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

// Tag moves tag to version: tags are unique, so any row already carrying it
// is cleared first. Run both statements in one transaction or under the lock.
func (s *Store) Tag(ctx context.Context, q Querier, version, tag string) error {
	if _, err := q.ExecContext(ctx, s.sql.clearTag, tag); err != nil {
		return fmt.Errorf("clear tag %s: %w", tag, err)
	}
	return s.exec(ctx, q, s.sql.setTag, tag, version)
}
