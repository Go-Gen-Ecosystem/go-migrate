// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"strconv"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/history"
)

func TestPlannerUpToDateDoesNotAllocateStatusRows(t *testing.T) {
	const n = 128
	cat := &catalog{units: make([]*unit, 0, n), byKey: make(map[string]*unit, n)}
	records := make([]history.Record, n)
	for i := range records {
		version := strconv.Itoa(i + 1)
		u := &unit{Migration: Migration{Version: version}, key: version, kind: history.KindGo}
		cat.units = append(cat.units, u)
		cat.byKey[version] = u
		records[i] = history.Record{Version: version, Kind: history.KindGo, Success: true}
	}
	p, s := planner{cat: cat}, newSnapshot(records)

	// Measure the production classifier with the catalog and snapshot already
	// built, so DB and fixture allocations are outside the observation.
	if allocs := testing.AllocsPerRun(10, func() {
		pending, err := p.up(s, "")
		if err != nil || len(pending) != 0 {
			t.Fatalf("up-to-date plan = %d pending, %v", len(pending), err)
		}
	}); allocs != 0 {
		t.Fatalf("up-to-date planner allocated %v times, want 0", allocs)
	}
}
