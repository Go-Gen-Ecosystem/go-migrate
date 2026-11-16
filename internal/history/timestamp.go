// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package history

import (
	"fmt"
	"time"
)

// timestamp scans a DATETIME/TIMESTAMP column whatever the driver returns, so
// the library works with DSNs it does not control: time.Time from drivers that
// parse times, or text from MySQL without parseTime=true and some SQLite
// drivers. Text without a zone is UTC, which is how rows are written.
type timestamp struct{ t *time.Time }

var timestampLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
}

// Scan implements sql.Scanner.
func (ts timestamp) Scan(v any) error {
	switch x := v.(type) {
	case time.Time:
		*ts.t = x
		return nil
	case []byte:
		return ts.parse(string(x))
	case string:
		return ts.parse(x)
	case nil:
		*ts.t = time.Time{}
		return nil
	default:
		return fmt.Errorf("installed_on: unsupported type %T", v)
	}
}

func (ts timestamp) parse(s string) error {
	for _, layout := range timestampLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			*ts.t = t
			return nil
		}
	}
	return fmt.Errorf("installed_on: unrecognised timestamp %q", s)
}
