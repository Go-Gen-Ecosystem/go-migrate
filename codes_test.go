// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestErrorCodes(t *testing.T) {
	if ErrorCodes(nil) != nil {
		t.Fatal("nil error has no codes")
	}
	if got := ErrorCodes(errors.New("boom")); !reflect.DeepEqual(got, []string{"error"}) {
		t.Fatalf("unknown = %v", got)
	}
	joined := errors.Join(fmt.Errorf("v1: %w", ErrChecksumMismatch), fmt.Errorf("v2: %w", ErrOutOfOrder))
	if got := ErrorCodes(joined); !reflect.DeepEqual(got, []string{"checksum_mismatch", "out_of_order"}) {
		t.Fatalf("joined = %v", got)
	}
	me := &MigrationError{Version: "3", Err: errors.New("syntax"), Dirty: true}
	if got := ErrorCodes(me); !reflect.DeepEqual(got, []string{"migration_failed", "dirty"}) {
		t.Fatalf("migration error = %v", got)
	}
	// Every sentinel has a unique code.
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c.code] {
			t.Fatalf("duplicate code %s", c.code)
		}
		seen[c.code] = true
	}
}
