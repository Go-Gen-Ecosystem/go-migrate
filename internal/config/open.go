package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
	_ "modernc.org/sqlite"             // registers "sqlite" (pure Go, no cgo)

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

// Connection defaults.
const (
	defaultConnectTimeout = 10 * time.Second
	defaultMaxOpenConns   = 4
	defaultPostgresPort   = 5432
	defaultMySQLPort      = 3306
	defaultHost           = "localhost"
	sqliteBusyTimeoutMS   = 10000
)

// driverNames maps a dialect to its registered database/sql driver.
var driverNames = map[string]string{
	dialect.Postgres: "pgx",
	dialect.MySQL:    "mysql",
	dialect.SQLite:   "sqlite",
}

// Open resolves the dialect and DSN, opens the pool and verifies it with a
// bounded ping. Errors never contain the DSN, which usually holds a password.
func Open(ctx context.Context, db Database) (*sql.DB, string, error) {
	name, dsn, err := Resolve(db)
	if err != nil {
		return nil, "", err
	}
	conn, err := sql.Open(driverNames[name], dsn)
	if err != nil {
		return nil, "", fmt.Errorf("open %s database: %w", name, err)
	}
	maxOpen := db.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = defaultMaxOpenConns
	}
	if name == dialect.SQLite && strings.Contains(dsn, ":memory:") {
		maxOpen = 1 // each connection to :memory: is a different database
	}
	conn.SetMaxOpenConns(maxOpen)
	conn.SetMaxIdleConns(maxOpen)

	timeout := db.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := conn.PingContext(pctx); err != nil {
		_ = conn.Close()
		return nil, "", fmt.Errorf("connect to %s database: %w", name, scrub(err, db.Password))
	}
	return conn, name, nil
}

// Resolve returns the dialect name and driver DSN for db. The driver is
// taken from db.Driver or inferred from the DSN (postgres://, mysql://,
// sqlite://, file:, *.db).
func Resolve(db Database) (string, string, error) {
	driver := db.Driver
	if driver == "" {
		driver = inferDriver(db.DSN)
	}
	if driver == "" {
		if db.DSN == "" && db.Name == "" {
			return "", "", ErrNoDatabase
		}
		return "", "", errors.New("cannot infer the database driver: set database.driver or --driver (postgres, mysql, sqlite)")
	}
	d, err := dialect.Lookup(driver)
	if err != nil {
		return "", "", err
	}
	var dsn string
	switch d.Name() {
	case dialect.Postgres:
		dsn, err = postgresDSN(db)
	case dialect.MySQL:
		dsn, err = mysqlDSN(db)
	default:
		dsn, err = sqliteDSN(db)
	}
	return d.Name(), dsn, err
}

func inferDriver(dsn string) string {
	lower := strings.ToLower(dsn)
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return dialect.Postgres
	case strings.HasPrefix(lower, "mysql://"):
		return dialect.MySQL
	case strings.HasPrefix(lower, "sqlite://"), strings.HasPrefix(lower, "sqlite3://"), strings.HasPrefix(lower, "file:"),
		strings.HasSuffix(lower, ".db"), strings.HasSuffix(lower, ".sqlite"), strings.HasSuffix(lower, ".sqlite3"),
		lower == ":memory:":
		return dialect.SQLite
	}
	return ""
}

func postgresDSN(db Database) (string, error) {
	if db.DSN != "" {
		return db.DSN, nil
	}
	if db.Name == "" {
		return "", ErrNoDatabase
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(or(db.Host, defaultHost), strconv.Itoa(orInt(db.Port, defaultPostgresPort))),
		Path:   "/" + db.Name,
	}
	if db.User != "" {
		u.User = url.UserPassword(db.User, db.Password)
	}
	q := url.Values{}
	for k, v := range db.Params {
		q.Set(k, v)
	}
	if db.SSLMode != "" {
		q.Set("sslmode", db.SSLMode)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// mysqlDSN accepts a go-sql-driver DSN, a mysql:// URL, or fields, and always
// sets parseTime=true and UTC so the history table's timestamps scan.
func mysqlDSN(db Database) (string, error) {
	var mc *mysqldriver.Config
	switch {
	case strings.HasPrefix(strings.ToLower(db.DSN), "mysql://"):
		u, err := url.Parse(db.DSN)
		if err != nil {
			return "", errors.New("invalid mysql:// URL")
		}
		mc = mysqldriver.NewConfig()
		mc.Net, mc.Addr, mc.DBName = "tcp", u.Host, strings.TrimPrefix(u.Path, "/")
		if u.Port() == "" {
			mc.Addr = net.JoinHostPort(u.Hostname(), strconv.Itoa(defaultMySQLPort))
		}
		if u.User != nil {
			mc.User = u.User.Username()
			mc.Passwd, _ = u.User.Password()
		}
		mc.Params = map[string]string{}
		for k, v := range u.Query() {
			mc.Params[k] = v[0]
		}
	case db.DSN != "":
		parsed, err := mysqldriver.ParseDSN(db.DSN)
		if err != nil {
			return "", errors.New("invalid mysql DSN") // the parser error can echo the DSN
		}
		mc = parsed
	default:
		if db.Name == "" {
			return "", ErrNoDatabase
		}
		mc = mysqldriver.NewConfig()
		mc.Net = "tcp"
		mc.Addr = net.JoinHostPort(or(db.Host, defaultHost), strconv.Itoa(orInt(db.Port, defaultMySQLPort)))
		mc.User, mc.Passwd, mc.DBName = db.User, db.Password, db.Name
	}
	if mc.Params == nil {
		mc.Params = map[string]string{}
	}
	for k, v := range db.Params {
		mc.Params[k] = v
	}
	mc.ParseTime = true
	mc.Loc = time.UTC
	return mc.FormatDSN(), nil
}

// sqliteDSN builds a modernc.org/sqlite DSN with a busy timeout and
// immediate write transactions, so concurrent writers wait instead of
// failing with SQLITE_BUSY.
func sqliteDSN(db Database) (string, error) {
	dsn := db.DSN
	if dsn == "" {
		dsn = db.Name
	}
	for _, p := range []string{"sqlite://", "sqlite3://"} {
		if len(dsn) >= len(p) && strings.EqualFold(dsn[:len(p)], p) {
			dsn = dsn[len(p):]
		}
	}
	if dsn == "" {
		return "", ErrNoDatabase
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	if !strings.Contains(dsn, "busy_timeout") {
		dsn += sep + "_pragma=busy_timeout(" + strconv.Itoa(sqliteBusyTimeoutMS) + ")"
		sep = "&"
	}
	if !strings.Contains(dsn, "_txlock") {
		dsn += sep + "_txlock=immediate"
	}
	return dsn, nil
}

// scrub removes a password from a driver error message, defensively.
func scrub(err error, password string) error {
	if password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), password, "****"))
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func orInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}
