// Package dialect describes the SQL differences between supported databases:
// identifier quoting, bind placeholders, lexical rules for statement
// splitting, DDL transactionality, history-table DDL and migration locking.
package dialect

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/go-migrate/internal/script"
)

// Names of the supported dialects.
const (
	Postgres = "postgres"
	MySQL    = "mysql"
	SQLite   = "sqlite"
)

// ErrUnsupported is returned by Lookup for an unknown dialect name.
var ErrUnsupported = errors.New("unsupported dialect")

// ErrInvalidIdentifier is returned for a table name that is not a plain
// (optionally schema-qualified) SQL identifier.
var ErrInvalidIdentifier = errors.New("invalid SQL identifier")

// Dialect holds everything that differs between databases. Values are
// immutable and safe for concurrent use.
type Dialect struct {
	name             string
	lexical          script.Lexical
	transactionalDDL bool
	quote            byte
	numbered         bool
	tsType           string
	tableSuffix      string
	existsSQL        string
	existsArgs       func(Table) []any
	locker           Locker
}

// Locker acquires the cross-process migration lock for key. The returned
// context is derived from ctx and is cancelled if the lock is lost; release
// must be called exactly once.
type Locker interface {
	Lock(ctx context.Context, db *sql.DB, key string, wait time.Duration) (context.Context, func(context.Context) error, error)
}

var dialects = map[string]*Dialect{
	Postgres: {
		name:             Postgres,
		lexical:          script.Lexical{DollarQuotes: true},
		transactionalDDL: true,
		quote:            '"',
		numbered:         true,
		tsType:           "TIMESTAMP",
		existsSQL:        "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = COALESCE($1, current_schema()) AND table_name = $2",
		existsArgs:       schemaAndName,
		locker:           pgLocker{},
	},
	MySQL: {
		name:        MySQL,
		lexical:     script.Lexical{Backticks: true, HashComments: true, BackslashEscapes: true},
		quote:       '`',
		tsType:      "DATETIME(6)",
		tableSuffix: " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		existsSQL:   "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = COALESCE(?, DATABASE()) AND table_name = ?",
		existsArgs:  schemaAndName,
		locker:      mysqlLocker{},
	},
	SQLite: {
		name:             SQLite,
		lexical:          script.Lexical{Backticks: true},
		transactionalDDL: true,
		quote:            '"',
		tsType:           "DATETIME",
		existsSQL:        "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		existsArgs:       func(t Table) []any { return []any{t.Name} },
		locker:           sqliteLocker{},
	},
}

// Lookup returns the dialect for a driver or dialect name. Common aliases
// (postgresql, pgx, mariadb, sqlite3) are accepted.
func Lookup(name string) (*Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pgx":
		return dialects[Postgres], nil
	case "mysql", "mariadb":
		return dialects[MySQL], nil
	case "sqlite", "sqlite3":
		return dialects[SQLite], nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupported, name)
	}
}

// Name returns the canonical dialect name.
func (d *Dialect) Name() string { return d.name }

// Lexical returns the statement-splitting rules for this dialect.
func (d *Dialect) Lexical() script.Lexical { return d.lexical }

// TransactionalDDL reports whether DDL can run inside a transaction and be
// rolled back. MySQL commits implicitly on every DDL statement.
func (d *Dialect) TransactionalDDL() bool { return d.transactionalDDL }

// Locker returns the dialect's migration lock implementation.
func (d *Dialect) Locker() Locker { return d.locker }

// Placeholder returns the bind placeholder for the n-th (1-based) argument.
func (d *Dialect) Placeholder(n int) string {
	if d.numbered {
		return "$" + strconv.Itoa(n)
	}
	return "?"
}

// Placeholders returns n comma-separated placeholders starting at 1.
func (d *Dialect) Placeholders(n int) string {
	ps := make([]string, n)
	for i := range ps {
		ps[i] = d.Placeholder(i + 1)
	}
	return strings.Join(ps, ", ")
}

var identPart = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// Table is a validated, optionally schema-qualified table name.
type Table struct {
	Schema string
	Name   string
}

// ParseTable validates name ("table" or "schema.table"). Only letters,
// digits and underscores are allowed, so the name is safe to interpolate
// into DDL after quoting.
func ParseTable(name string) (Table, error) {
	parts := strings.Split(name, ".")
	if len(parts) > 2 {
		return Table{}, fmt.Errorf("%w: %q", ErrInvalidIdentifier, name)
	}
	for _, p := range parts {
		if !identPart.MatchString(p) {
			return Table{}, fmt.Errorf("%w: %q", ErrInvalidIdentifier, name)
		}
	}
	if len(parts) == 2 {
		return Table{Schema: parts[0], Name: parts[1]}, nil
	}
	return Table{Name: parts[0]}, nil
}

// String returns the unquoted, dotted name.
func (t Table) String() string {
	if t.Schema == "" {
		return t.Name
	}
	return t.Schema + "." + t.Name
}

// Quote returns t quoted for this dialect.
func (d *Dialect) Quote(t Table) string {
	q := string(d.quote)
	if t.Schema == "" {
		return q + t.Name + q
	}
	return q + t.Schema + q + "." + q + t.Name + q
}

// HistoryDDL returns the statement that creates the history table if absent.
// Version uniqueness is a named table constraint, which is portable across
// all three dialects (unlike partial or IF NOT EXISTS indexes).
func (d *Dialect) HistoryDDL(t Table) string {
	q := string(d.quote)
	return "CREATE TABLE IF NOT EXISTS " + d.Quote(t) + " (\n" +
		"  installed_rank INTEGER NOT NULL PRIMARY KEY,\n" +
		"  version VARCHAR(128) NOT NULL,\n" +
		"  description VARCHAR(255) NOT NULL,\n" +
		"  kind VARCHAR(16) NOT NULL,\n" +
		"  script VARCHAR(1024) NOT NULL,\n" +
		"  checksum VARCHAR(64) NOT NULL,\n" +
		"  installed_by VARCHAR(255) NOT NULL,\n" +
		"  installed_on " + d.tsType + " NOT NULL,\n" +
		"  execution_ms BIGINT NOT NULL,\n" +
		"  success BOOLEAN NOT NULL,\n" +
		"  CONSTRAINT " + q + t.Name + "_version_uk" + q + " UNIQUE (version)\n" +
		")" + d.tableSuffix
}

// TableExists reports whether t exists, without creating it.
func (d *Dialect) TableExists(ctx context.Context, db *sql.DB, t Table) (bool, error) {
	var n int
	if err := db.QueryRowContext(ctx, d.existsSQL, d.existsArgs(t)...).Scan(&n); err != nil {
		return false, fmt.Errorf("check table %s: %w", t, err)
	}
	return n > 0, nil
}

// schemaAndName passes a NULL schema for unqualified tables so the query
// falls back to the connection's current schema/database.
func schemaAndName(t Table) []any {
	var schema any
	if t.Schema != "" {
		schema = t.Schema
	}
	return []any{schema, t.Name}
}
