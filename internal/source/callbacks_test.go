// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package source

import "testing"

func TestLoadAllCallbacks(t *testing.T) {
	set, err := LoadAll(mapFS(map[string]string{
		"1_a.sql":                   "SELECT 1;",
		"afterMigrate__b.sql":       "SELECT 'b';",
		"AfterMigrate__a.sql":       "SELECT 'a';",
		"beforeEachMigrate.sql":     "SELECT 'e';",
		"sub/afterMigrateError.sql": "SELECT 'x';",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || len(set.Callbacks) != 4 {
		t.Fatalf("files=%d callbacks=%d", len(set.Files), len(set.Callbacks))
	}
	if set.Callbacks[0].Name != "AfterMigrate__a.sql" || set.Callbacks[0].Event != AfterMigrate {
		t.Fatalf("callbacks must be ordered by name with canonical events: %+v", set.Callbacks[0])
	}
}
