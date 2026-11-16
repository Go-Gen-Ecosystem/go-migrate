package history

import (
	"testing"
	"time"
)

func TestTimestampScan(t *testing.T) {
	want := time.Date(2026, 11, 16, 20, 1, 2, 123456000, time.UTC)
	for _, in := range []any{
		want,
		[]byte("2026-11-16 20:01:02.123456"), // MySQL DATETIME(6) without parseTime
		"2026-11-16 20:01:02.123456+00:00",   // modernc sqlite text
		"2026-11-16T20:01:02.123456Z",        // RFC 3339
		"2026-11-16 22:01:02.123456+02:00",   // offset preserved as instant
	} {
		var got time.Time
		if err := (timestamp{&got}).Scan(in); err != nil {
			t.Fatalf("Scan(%v): %v", in, err)
		}
		if !got.Equal(want) {
			t.Errorf("Scan(%v) = %v, want %v", in, got, want)
		}
	}
	var got time.Time
	if err := (timestamp{&got}).Scan(nil); err != nil || !got.IsZero() {
		t.Errorf("Scan(nil) = %v, %v", got, err)
	}
	if err := (timestamp{&got}).Scan("not a time"); err == nil {
		t.Error("garbage must fail")
	}
	if err := (timestamp{&got}).Scan(42); err == nil {
		t.Error("unsupported type must fail")
	}
}
