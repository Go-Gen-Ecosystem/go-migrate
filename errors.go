// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
	"github.com/go-gen-ecosystem/go-migrate/internal/history"
	"github.com/go-gen-ecosystem/go-migrate/internal/script"
	"github.com/go-gen-ecosystem/go-migrate/internal/source"
)

// Sentinel errors. Use errors.Is to test for them; most are wrapped with the
// offending version.
var (
	// ErrDirty means a non-transactional migration failed part-way and left
	// a history row with success = false. Bring the schema by hand to either
	// side of the migration, then run Repair (schema as before it: the row
	// is removed) or MarkApplied (schema as after it: the row is completed).
	ErrDirty = errors.New("database is dirty: a migration failed part-way; " + dirtyGuidance)
	// ErrBelowBaseline means a rollback target lies below the baseline,
	// which cannot be rolled back.
	ErrBelowBaseline = errors.New("target version is below the baseline")
	// ErrChecksumMismatch means an applied migration file was edited.
	ErrChecksumMismatch = errors.New("applied migration was modified")
	// ErrMissingMigration means the database has a version that no longer
	// exists in the migration source.
	ErrMissingMigration = errors.New("applied migration is missing from the source")
	// ErrOutOfOrder means a pending migration has a lower version than the
	// latest applied one; enable WithAllowOutOfOrder to apply it anyway.
	ErrOutOfOrder = errors.New("pending migration is older than the latest applied version")
	// ErrTransactionControl means a migration that runs in a transaction
	// contains BEGIN/COMMIT/ROLLBACK; mark it "-- +migrate Up notransaction".
	ErrTransactionControl = errors.New("transaction control statement in a transactional migration (use notransaction)")
	// ErrUnresolvedPlaceholder means an SQL file references ${name} that was
	// not provided via WithPlaceholders.
	ErrUnresolvedPlaceholder = errors.New("unresolved placeholder")
	// ErrPreconditionFailed means a migration's precondition with
	// onFail=HALT did not hold; nothing of that migration ran.
	ErrPreconditionFailed = errors.New("precondition failed")
	// ErrTagNotFound means no applied migration carries the requested tag.
	ErrTagNotFound = errors.New("tag not found")
	// ErrInvalidArgument means a caller passed invalid or conflicting arguments.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrPlanChanged means the plan to execute no longer matches the one
	// approved (ExpectPlan): migrations were added, edited or applied since.
	ErrPlanChanged = errors.New("plan changed since it was approved")
	// ErrInvalidVersion means a version is not numeric (digits, optionally dot-separated).
	ErrInvalidVersion = errors.New("invalid version")
	// ErrVersionNotFound means a requested target version does not exist.
	ErrVersionNotFound = errors.New("version not found")
	// ErrIrreversible means a migration that must be rolled back has no down section.
	ErrIrreversible = errors.New("migration has no down section")
	// ErrNotEnoughApplied means Down was asked to roll back more migrations than are applied.
	ErrNotEnoughApplied = errors.New("not enough applied migrations")
	// ErrBaselineNotEmpty means Baseline was called on a database with history.
	ErrBaselineNotEmpty = errors.New("baseline requires an empty history table")
	// ErrLockTimeout means another process held the migration lock for longer than the lock wait.
	ErrLockTimeout = dialect.ErrLockTimeout
	// ErrLockLost means the migration lock expired or was taken over while held.
	ErrLockLost = dialect.ErrLockLost
)

// Errors from loading migrations (New) and from the database layer,
// re-exported so callers can test for them with errors.Is.
var (
	ErrBadPrecondition     = script.ErrBadPrecondition
	ErrBadFileName         = source.ErrBadFileName
	ErrDuplicateVersion    = source.ErrDuplicateVersion
	ErrMissingUp           = source.ErrMissingUp
	ErrEmptyUp             = source.ErrEmptyUp
	ErrUpInDownFile        = source.ErrUpInDownFile
	ErrMisplacedDirective  = source.ErrMisplacedDirective
	ErrContentBeforeMarker = script.ErrContentBeforeMarker
	ErrDuplicateMarker     = script.ErrDuplicateMarker
	ErrUnbalancedBlock     = script.ErrUnbalancedBlock
	ErrUnknownDirective    = script.ErrUnknownDirective
	ErrTSQLBatch           = script.ErrTSQLBatch
	ErrPLSQLBlock          = script.ErrPLSQLBlock
	ErrSQLPLBlock          = script.ErrSQLPLBlock
	ErrUnsupportedDialect  = dialect.ErrUnsupported
	ErrInvalidIdentifier   = dialect.ErrInvalidIdentifier
	ErrHistoryRowNotFound  = history.ErrNotFound
)

// maxSQLInError bounds how much of a failing statement is echoed in errors,
// so large seed statements are not copied into logs wholesale.
const maxSQLInError = 120

// MigrationError reports which migration and statement failed.
type MigrationError struct {
	Version   string
	Direction Direction
	// Statement is the 1-based index of the failing statement (0 for Go migrations).
	Statement int
	// SQL is the start of the failing statement, truncated.
	SQL string
	// Dirty is true when the failure left the database in a partial state
	// (non-transactional migration); see ErrDirty.
	Dirty bool
	Err   error
}

// dirtyGuidance says how to recover from a dirty row in either direction:
// the row is resolved by the state the schema is left in, not by which
// direction failed.
const dirtyGuidance = "make the schema consistent by hand, then run repair if the migration is now not applied, or mark-applied if it is"

func (e *MigrationError) Error() string {
	msg := fmt.Sprintf("migration %s %s failed", e.Version, e.Direction)
	if e.Statement > 0 {
		msg += fmt.Sprintf(" at statement %d (%s)", e.Statement, e.SQL)
	}
	msg += ": " + e.Err.Error()
	if e.Dirty {
		msg += " [database left dirty: " + dirtyGuidance + "]"
	}
	return msg
}

// Unwrap returns the underlying cause (and ErrDirty when Dirty is set).
func (e *MigrationError) Unwrap() []error {
	if e.Dirty {
		return []error{e.Err, ErrDirty}
	}
	return []error{e.Err}
}

// truncateSQL returns a one-line excerpt of a statement for error messages:
// leading comment lines are skipped (they usually repeat the file header),
// whitespace is collapsed and the result is capped at maxSQLInError runes.
// The cap limits, but cannot rule out, sensitive literals in the excerpt.
func truncateSQL(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 1 && strings.HasPrefix(strings.TrimSpace(lines[0]), "--") {
		lines = lines[1:]
	}
	r := []rune(strings.Join(strings.Fields(strings.Join(lines, " ")), " "))
	if len(r) <= maxSQLInError {
		return string(r)
	}
	return string(r[:maxSQLInError]) + "…"
}
