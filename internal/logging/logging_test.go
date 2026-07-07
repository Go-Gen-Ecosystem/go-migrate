/**
 * @file logging_test.go
 * @description Tests for the reconstructed component-logger package.
 * @author Admilson B. F. Cossa
 */

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogLevel_String(t *testing.T) {
	cases := map[LogLevel]string{
		LevelDebug:   "DEBUG",
		LevelInfo:    "INFO",
		LevelWarn:    "WARN",
		LevelError:   "ERROR",
		LevelFatal:   "FATAL",
		LogLevel(99): "INFO",
	}
	for lvl, want := range cases {
		if got := lvl.String(); got != want {
			t.Errorf("LogLevel(%d).String() = %q, want %q", lvl, got, want)
		}
	}
}

func TestNew_JSONOutputAndLevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo, "json")

	l.Debug("debug msg") // below level: filtered
	l.Info("info msg", "key", "value")

	out := buf.String()
	if strings.Contains(out, "debug msg") {
		t.Error("debug below the configured level should be filtered out")
	}
	if !strings.Contains(out, "info msg") {
		t.Errorf("info message missing from output: %q", out)
	}
	// The line is valid JSON with the field attached.
	line := strings.TrimSpace(out)
	var rec map[string]interface{}
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("output is not valid JSON: %v (%q)", err, line)
	}
	if rec["key"] != "value" {
		t.Errorf("field key/value missing: %v", rec)
	}
}

func TestAllLevelsAndFormatted(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelDebug, "text")
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	l.Debugf("d%d", 1)
	l.Infof("i%d", 2)
	l.Warnf("w%d", 3)
	l.Errorf("e%d", 4)
	for _, want := range []string{"d", "i", "w", "e", "d1", "i2", "w3", "e4"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("missing %q in output", want)
		}
	}
}

// TestFatal_LogsWithoutExiting asserts that the library logger's Fatal/Fatalf
// emit at FATAL severity but return normally rather than terminating the
// process. If Fatal called os.Exit, the statements after it would never run and
// the test binary would abort, so reaching the assertions proves no exit.
func TestFatal_LogsWithoutExiting(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelDebug, "json")

	l.Fatal("fatal-msg", "k", "v")
	l.Fatalf("fatalf-%d", 7)

	// Reaching here means neither call exited the process.
	out := buf.String()
	if !strings.Contains(out, "fatal-msg") {
		t.Errorf("Fatal message missing from output: %q", out)
	}
	if !strings.Contains(out, "fatalf-7") {
		t.Errorf("Fatalf message missing from output: %q", out)
	}
	// Fatal attaches structured fields like any other level.
	if !strings.Contains(out, "\"k\":\"v\"") {
		t.Errorf("Fatal structured field missing: %q", out)
	}
}

func TestWith_AttachesFields(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo, "json").With("component", "svc")
	l.Info("hello")
	if !strings.Contains(buf.String(), "\"component\":\"svc\"") {
		t.Errorf("With field not attached: %q", buf.String())
	}
}

func TestWith_OddFieldsTolerated(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo, "json")
	// odd number of key/value args must not panic
	l.Info("msg", "loneKey")
	if !strings.Contains(buf.String(), "msg") {
		t.Error("message should still be logged with odd fields")
	}
}

func TestWithContext_ContextAndKeyValues(t *testing.T) {
	var buf bytes.Buffer
	base := New(&buf, LevelInfo, "json")

	// context form: returns a usable logger, no fields added
	l1 := base.WithContext(context.Background())
	l1.Info("ctx form")
	if !strings.Contains(buf.String(), "ctx form") {
		t.Error("WithContext(ctx) logger should log")
	}

	buf.Reset()
	// key/value form: attaches fields
	l2 := base.WithContext("request_id", "abc")
	l2.Info("kv form")
	if !strings.Contains(buf.String(), "\"request_id\":\"abc\"") {
		t.Errorf("WithContext(kv) should attach fields: %q", buf.String())
	}
}

func TestConstructors(t *testing.T) {
	if NewSimpleLogger() == nil {
		t.Error("NewSimpleLogger nil")
	}
	if NewZapLogger("debug", true) == nil || NewZapLogger("warn", false) == nil {
		t.Error("NewZapLogger nil")
	}
	if NewNoOpLogger() == nil {
		t.Error("NewNoOpLogger nil")
	}
	if NewComponentLogger("svc", LevelInfo) == nil {
		t.Error("NewComponentLogger nil")
	}
}

func TestNoOpLoggerDiscards(t *testing.T) {
	// Must not panic and must not write anywhere observable.
	l := NewNoOpLogger()
	l.Info("nothing", "k", "v")
	l.Error("nothing")
}

func TestGetComponentLogger_Caches(t *testing.T) {
	a := GetComponentLogger("cache-test")
	b := GetComponentLogger("cache-test")
	if a != b {
		t.Error("GetComponentLogger should return a cached instance per component")
	}
	if GetComponentLogger("other") == a {
		t.Error("different components should get different loggers")
	}
}
