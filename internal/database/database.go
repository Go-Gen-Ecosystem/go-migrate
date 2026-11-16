// Package database opens the GORM connection the migration engine runs on.
//
// It owns three concerns and nothing else: mapping a configured driver name to
// a supported [Dialect], turning a [types.DatabaseConfig] into a driver DSN
// (either the explicit DSN or one built from host/port/credential fields), and
// opening a pinged, pool-configured connection. Errors never include the DSN,
// because it usually carries credentials.
package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// Dialect identifies a supported SQL database family.
type Dialect string

// Supported dialects. The values match GORM's Dialector.Name().
const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
	MySQL    Dialect = "mysql"
)

// Connection defaults, used when the config leaves a value at zero.
const (
	defaultConnectTimeout  = 10 * time.Second
	defaultMaxOpenConns    = 4
	defaultMaxIdleConns    = 2
	defaultPostgresPort    = 5432
	defaultMySQLPort       = 3306
	defaultHost            = "localhost"
	sqliteBusyTimeoutParam = "_busy_timeout"
	sqliteBusyTimeoutMS    = 5000
)

// ErrUnsupportedDriver is returned for a driver name with no known dialect.
var ErrUnsupportedDriver = errors.New("unsupported database driver")

// ErrMissingDatabase is returned when neither a DSN nor a database name/path is configured.
var ErrMissingDatabase = errors.New("database config needs either dsn or database")

// ParseDialect maps a configured driver name (including common aliases) to a Dialect.
func ParseDialect(driver string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "sqlite", "sqlite3":
		return SQLite, nil
	case "postgres", "postgresql", "pgx":
		return Postgres, nil
	case "mysql", "mariadb":
		return MySQL, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDriver, driver)
	}
}

// DialectOf reports the dialect of an open GORM connection.
func DialectOf(db *gorm.DB) Dialect {
	return Dialect(db.Name())
}

// TransactionalDDL reports whether schema changes can be rolled back inside a
// transaction. MySQL commits implicitly on every DDL statement.
func (d Dialect) TransactionalDDL() bool {
	return d != MySQL
}

// Connect opens a connection for cfg, applies pool settings and verifies it
// with a ping bounded by cfg.ConnectionTimeout (default 10s) and ctx.
func Connect(ctx context.Context, cfg types.DatabaseConfig) (*gorm.DB, error) {
	dialect, err := ParseDialect(cfg.Driver)
	if err != nil {
		return nil, err
	}
	dsn, err := ResolveDSN(dialect, cfg)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialectorFor(dialect, dsn), &gorm.Config{
		Logger:               gormlogger.Discard,
		DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s connection: %w", dialect, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("obtain %s connection pool: %w", dialect, err)
	}

	maxOpen := positiveOr(cfg.MaxConnections, defaultMaxOpenConns)
	if dialect == SQLite && isSQLiteMemory(dsn) {
		// Every connection to a private in-memory database is a different
		// database; pin the pool to one connection so state is shared.
		maxOpen = 1
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(min(maxOpen, defaultMaxIdleConns))
	if cfg.MaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.MaxLifetime)
	}
	if cfg.IdleTimeout > 0 {
		sqlDB.SetConnMaxIdleTime(cfg.IdleTimeout)
	}

	pingCtx, cancel := context.WithTimeout(ctx, durationOr(cfg.ConnectionTimeout, defaultConnectTimeout))
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("connect to %s database: %w", dialect, err)
	}
	return db, nil
}

// ResolveDSN returns the driver DSN for cfg. An explicit DSN wins; otherwise
// one is built from the individual connection fields. MySQL DSNs are always
// normalised to parseTime=true with UTC so timestamp columns scan correctly.
func ResolveDSN(dialect Dialect, cfg types.DatabaseConfig) (string, error) {
	switch dialect {
	case SQLite:
		return sqliteDSN(cfg)
	case Postgres:
		return postgresDSN(cfg)
	case MySQL:
		return mysqlDSN(cfg)
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedDriver, dialect)
	}
}

func sqliteDSN(cfg types.DatabaseConfig) (string, error) {
	dsn := cfg.DSN
	if dsn == "" {
		dsn = cfg.Database
	}
	if dsn == "" {
		return "", ErrMissingDatabase
	}
	if strings.Contains(dsn, sqliteBusyTimeoutParam) {
		return dsn, nil
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	// A busy timeout makes concurrent writers wait instead of failing
	// immediately with SQLITE_BUSY.
	return dsn + sep + sqliteBusyTimeoutParam + "=" + strconv.Itoa(sqliteBusyTimeoutMS), nil
}

func postgresDSN(cfg types.DatabaseConfig) (string, error) {
	if cfg.DSN != "" {
		return cfg.DSN, nil
	}
	if cfg.Database == "" {
		return "", ErrMissingDatabase
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(stringOr(cfg.Host, defaultHost), strconv.Itoa(positiveOr(cfg.Port, defaultPostgresPort))),
		Path:   "/" + cfg.Database,
	}
	if cfg.Username != "" {
		u.User = url.UserPassword(cfg.Username, cfg.Password)
	}
	q := url.Values{}
	for k, v := range cfg.Options {
		q.Set(k, v)
	}
	if cfg.SSLMode != "" {
		q.Set("sslmode", cfg.SSLMode)
	}
	if cfg.Timezone != "" {
		q.Set("TimeZone", cfg.Timezone)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func mysqlDSN(cfg types.DatabaseConfig) (string, error) {
	var mc *mysqldriver.Config
	if cfg.DSN != "" {
		parsed, err := mysqldriver.ParseDSN(cfg.DSN)
		if err != nil {
			// The parser error may echo the DSN; do not wrap it.
			return "", errors.New("invalid mysql dsn")
		}
		mc = parsed
	} else {
		if cfg.Database == "" {
			return "", ErrMissingDatabase
		}
		mc = mysqldriver.NewConfig()
		mc.Net = "tcp"
		mc.Addr = net.JoinHostPort(stringOr(cfg.Host, defaultHost), strconv.Itoa(positiveOr(cfg.Port, defaultMySQLPort)))
		mc.User = cfg.Username
		mc.Passwd = cfg.Password
		mc.DBName = cfg.Database
	}
	if mc.Params == nil {
		mc.Params = map[string]string{}
	}
	for k, v := range cfg.Options {
		mc.Params[k] = v
	}
	if cfg.Charset != "" {
		mc.Params["charset"] = cfg.Charset
	}
	mc.ParseTime = true
	mc.Loc = time.UTC
	return mc.FormatDSN(), nil
}

func dialectorFor(dialect Dialect, dsn string) gorm.Dialector {
	switch dialect {
	case Postgres:
		return postgres.Open(dsn)
	case MySQL:
		return mysql.Open(dsn)
	default:
		return sqlite.Open(dsn)
	}
}

func isSQLiteMemory(dsn string) bool {
	return strings.Contains(dsn, ":memory:") || strings.Contains(dsn, "mode=memory")
}

func positiveOr(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func durationOr(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}

func stringOr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
