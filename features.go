// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

// placeholderRef matches ${name}; names may contain letters, digits, '_',
// '.', ':' and '-' (built-ins use the "migrate:" prefix).
var placeholderRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_.:\-]*)\}`)

// expandPlaceholders substitutes ${name} references. A nil map disables
// substitution entirely, so SQL that legitimately contains "${" is untouched
// unless placeholders were enabled.
func expandPlaceholders(s string, vals map[string]string) (string, error) {
	if vals == nil {
		return s, nil
	}
	var missing string
	out := placeholderRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := ref[2 : len(ref)-1]
		v, ok := vals[name]
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("${%s}: %w", missing, ErrUnresolvedPlaceholder)
	}
	return out, nil
}

func expandAll(stmts []string, vals map[string]string) ([]string, error) {
	if vals == nil {
		return stmts, nil
	}
	out := make([]string, len(stmts))
	for i, s := range stmts {
		var err error
		if out[i], err = expandPlaceholders(s, vals); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// matches implements "declared nothing, or nothing selected, or overlap".
func matches(declared, active []string) bool {
	if len(declared) == 0 || len(active) == 0 {
		return true
	}
	for _, d := range declared {
		if slices.Contains(active, d) {
			return true
		}
	}
	return false
}

// precondition is a resolved script.Precondition.
type precondition struct {
	sql    string
	expect string
	onFail script.OnFail
}

func resolvePreconditions(pcs []script.Precondition, vals map[string]string) ([]precondition, error) {
	out := make([]precondition, 0, len(pcs))
	for _, pc := range pcs {
		q, err := expandPlaceholders(pc.SQL, vals)
		if err != nil {
			return nil, err
		}
		out = append(out, precondition{sql: q, expect: pc.Expect, onFail: pc.OnFail})
	}
	return out, nil
}

// checkPreconditions evaluates pcs in order and returns the OnFail action of
// the first one that does not hold, or "" when all hold.
func checkPreconditions(ctx context.Context, q Executor, pcs []precondition) (script.OnFail, error) {
	for _, pc := range pcs {
		var v any
		if err := q.QueryRowContext(ctx, pc.sql).Scan(&v); err != nil {
			return "", fmt.Errorf("precondition (%s): %w", truncateSQL(pc.sql), err)
		}
		if scalar(v) != pc.expect {
			return pc.onFail, nil
		}
	}
	return "", nil
}

// scalar renders a driver value the way it would be written in SQL.
func scalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case []byte:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

// callback is one resolved SQL callback file.
type callback struct {
	name  string
	stmts []string
}
