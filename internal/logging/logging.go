/**
 * @file logging.go
 * @description Self-contained structured logging for go-migrate. Reconstructs
 * the component-logger API the migration engine was written against (it
 * previously lived in the gogen monorepo's observability/logging package).
 * @author Admilson B. F. Cossa
 */

package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// LogLevel is the severity of a log entry.
type LogLevel int

// Log levels in increasing severity.
const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the level name.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "INFO"
	}
}

// Logger is the structured logging contract used across go-migrate. Field
// arguments are variadic key/value pairs (msg, "key1", val1, "key2", val2, ...).
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
	Fatal(msg string, fields ...interface{})

	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})

	// With returns a logger that includes the given key/value fields.
	With(fields ...interface{}) Logger
	// WithContext returns a logger enriched from a context.Context or from
	// variadic key/value pairs (both call styles are supported).
	WithContext(args ...interface{}) Logger
}

// ComponentLogger is a Logger bound to a named component.
type ComponentLogger = Logger

// slogLevel maps a LogLevel to slog.Level.
func slogLevel(l LogLevel) slog.Level {
	switch l {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError, LevelFatal:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logger is the slog-backed Logger implementation.
type logger struct {
	slog  *slog.Logger
	level LogLevel
}

// New creates a Logger writing to w at the given level in the given format
// ("json" or "text").
func New(w io.Writer, level LogLevel, format string) Logger {
	opts := &slog.HandlerOptions{Level: slogLevel(level)}
	var h slog.Handler
	switch format {
	case "text", "console":
		h = slog.NewTextHandler(w, opts)
	default:
		h = slog.NewJSONHandler(w, opts)
	}
	return &logger{slog: slog.New(h), level: level}
}

// NewSimpleLogger returns a Logger writing JSON to stdout at info level.
func NewSimpleLogger() Logger { return New(os.Stdout, LevelInfo, "json") }

// NewZapLogger returns a Logger for the given level name ("debug"/"info"/...)
// and development flag (text output when true, JSON otherwise). The name mirrors
// the historical constructor; the implementation is slog-based.
func NewZapLogger(level string, development bool) Logger {
	lvl := LevelInfo
	switch level {
	case "debug":
		lvl = LevelDebug
	case "warn":
		lvl = LevelWarn
	case "error":
		lvl = LevelError
	}
	format := "json"
	if development {
		format = "text"
	}
	return New(os.Stdout, lvl, format)
}

// NewNoOpLogger returns a Logger that discards everything.
func NewNoOpLogger() Logger {
	return New(io.Discard, LevelError, "json")
}

// NewComponentLogger returns a Logger tagged with the component name at the
// given level.
func NewComponentLogger(component string, level LogLevel) ComponentLogger {
	return New(os.Stdout, level, "json").With("component", component)
}

var (
	globalMu    sync.Mutex
	globalCache = map[string]ComponentLogger{}
)

// GetComponentLogger returns a process-wide cached logger for the component.
func GetComponentLogger(component string) ComponentLogger {
	globalMu.Lock()
	defer globalMu.Unlock()
	if l, ok := globalCache[component]; ok {
		return l
	}
	l := NewComponentLogger(component, LevelInfo)
	globalCache[component] = l
	return l
}

func (l *logger) log(level LogLevel, msg string, fields ...interface{}) {
	if level < l.level {
		return
	}
	l.slog.Log(context.Background(), slogLevel(level), msg, normalize(fields)...)
}

// normalize coerces variadic fields into slog-friendly key/value pairs,
// tolerating odd counts and non-string keys.
func normalize(fields []interface{}) []interface{} {
	if len(fields)%2 == 0 {
		return fields
	}
	return append(fields, "(MISSING)")
}

func (l *logger) Debug(msg string, f ...interface{}) { l.log(LevelDebug, msg, f...) }
func (l *logger) Info(msg string, f ...interface{})  { l.log(LevelInfo, msg, f...) }
func (l *logger) Warn(msg string, f ...interface{})  { l.log(LevelWarn, msg, f...) }
func (l *logger) Error(msg string, f ...interface{}) { l.log(LevelError, msg, f...) }

// Fatal logs at fatal severity and exits the process.
func (l *logger) Fatal(msg string, f ...interface{}) {
	l.log(LevelFatal, msg, f...)
	os.Exit(1)
}

func (l *logger) Debugf(format string, a ...interface{}) { l.log(LevelDebug, fmt.Sprintf(format, a...)) }
func (l *logger) Infof(format string, a ...interface{})  { l.log(LevelInfo, fmt.Sprintf(format, a...)) }
func (l *logger) Warnf(format string, a ...interface{})  { l.log(LevelWarn, fmt.Sprintf(format, a...)) }
func (l *logger) Errorf(format string, a ...interface{}) { l.log(LevelError, fmt.Sprintf(format, a...)) }

// Fatalf logs a formatted message at fatal severity and exits.
func (l *logger) Fatalf(format string, a ...interface{}) {
	l.log(LevelFatal, fmt.Sprintf(format, a...))
	os.Exit(1)
}

// With returns a logger carrying the given key/value fields.
func (l *logger) With(fields ...interface{}) Logger {
	return &logger{slog: l.slog.With(normalize(fields)...), level: l.level}
}

// WithContext accepts either a context.Context or variadic key/value pairs.
// A context contributes no fields by default (there is no ambient field
// registry); key/value pairs are attached like With.
func (l *logger) WithContext(args ...interface{}) Logger {
	if len(args) == 1 {
		if _, ok := args[0].(context.Context); ok {
			return l
		}
	}
	return l.With(args...)
}
