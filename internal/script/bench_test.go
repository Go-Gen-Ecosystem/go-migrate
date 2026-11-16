// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package script

import (
	"strings"
	"testing"
)

// benchScript is a realistic mix: DDL, DML with quoted ';', comments and a
// dollar-quoted function body.
const benchUnit = `-- create table; comment with semicolon
CREATE TABLE accounts (id BIGINT PRIMARY KEY, name TEXT NOT NULL, note TEXT DEFAULT 'a;b');
INSERT INTO accounts VALUES (1, 'it''s; fine', /* inline; comment */ 'x');
CREATE FUNCTION touch() RETURNS trigger AS $$ BEGIN NEW.note := 'touched;'; RETURN NEW; END; $$ LANGUAGE plpgsql;
CREATE INDEX accounts_name ON accounts (name);
`

func BenchmarkSplit(b *testing.B) {
	src := strings.Repeat(benchUnit, 2000) // ~640 KB, 8000 statements
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if n := len(Split(src, Lexical{DollarQuotes: true})); n != 8000 {
			b.Fatalf("statements = %d", n)
		}
	}
}

func BenchmarkParse(b *testing.B) {
	src := []byte("-- +migrate Up\n" + strings.Repeat(benchUnit, 20) + "-- +migrate Down\nDROP TABLE accounts;\n")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse(src, Up); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChecksum(b *testing.B) {
	src := []byte(strings.Repeat(benchUnit, 200))
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		_ = Checksum(src)
	}
}
