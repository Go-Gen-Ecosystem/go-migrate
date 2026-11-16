// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import "errors"

// codes maps every sentinel error to a stable, snake_case code. Codes are a
// public contract for CI pipelines and AI agents that branch on outcomes:
// they never change once released, while messages may.
var codes = []struct {
	err  error
	code string
}{
	{ErrDirty, "dirty"},
	{ErrChecksumMismatch, "checksum_mismatch"},
	{ErrMissingMigration, "missing_migration"},
	{ErrOutOfOrder, "out_of_order"},
	{ErrVersionNotFound, "version_not_found"},
	{ErrIrreversible, "irreversible"},
	{ErrNotEnoughApplied, "not_enough_applied"},
	{ErrBaselineNotEmpty, "baseline_not_empty"},
	{ErrBelowBaseline, "below_baseline"},
	{ErrLockTimeout, "lock_timeout"},
	{ErrLockLost, "lock_lost"},
	{ErrTransactionControl, "transaction_control"},
	{ErrUnresolvedPlaceholder, "unresolved_placeholder"},
	{ErrPreconditionFailed, "precondition_failed"},
	{ErrTagNotFound, "tag_not_found"},
	{ErrInvalidVersion, "invalid_version"},
	{ErrInvalidArgument, "invalid_argument"},
	{ErrPlanChanged, "plan_changed"},
	{ErrBadPrecondition, "bad_precondition"},
	{ErrBadFileName, "bad_file_name"},
	{ErrDuplicateVersion, "duplicate_version"},
	{ErrMissingUp, "missing_up"},
	{ErrEmptyUp, "empty_up"},
	{ErrUpInDownFile, "up_in_down_file"},
	{ErrMisplacedDirective, "misplaced_directive"},
	{ErrContentBeforeMarker, "content_before_marker"},
	{ErrDuplicateMarker, "duplicate_marker"},
	{ErrUnbalancedBlock, "unbalanced_block"},
	{ErrUnknownDirective, "unknown_directive"},
	{ErrTSQLBatch, "invalid_tsql_batch"},
	{ErrPLSQLBlock, "invalid_plsql_block"},
	{ErrUnsupportedDialect, "unsupported_dialect"},
	{ErrInvalidIdentifier, "invalid_identifier"},
	{ErrHistoryRowNotFound, "history_row_not_found"},
}

// ErrorCodes returns the stable codes of every known error in err's tree, in
// a fixed order, plus "migration_failed" when a migration's SQL or Go code
// failed. It returns nil for nil and ["error"] for anything unrecognised.
func ErrorCodes(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	var me *MigrationError
	if errors.As(err, &me) {
		out = append(out, "migration_failed")
	}
	for _, c := range codes {
		if errors.Is(err, c.err) {
			out = append(out, c.code)
		}
	}
	if len(out) == 0 {
		out = []string{"error"}
	}
	return out
}
