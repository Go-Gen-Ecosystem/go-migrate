// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

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
	_ "github.com/jackc/pgx/v5/stdlib"      // registers "pgx"
	mssql "github.com/microsoft/go-mssqldb" // registers "sqlserver"
	_ "modernc.org/sqlite"                  // registers "sqlite" (pure Go, no cgo)

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

// Connection defaults.
const (
	defaultConnectTimeout = 10 * time.Second
	defaultMaxOpenConns   = 4
	defaultPostgresPort   = 5432
	defaultMySQLPort      = 3306
	defaultSQLServerPort  = 1433
	defaultHost           = "localhost"
	sqliteBusyTimeoutMS   = 10000
)

// driverNames maps a dialect to its registered database/sql driver.
var driverNames = map[string]string{
	dialect.Postgres:  "pgx",
	dialect.MySQL:     "mysql",
	dialect.SQLite:    "sqlite",
	dialect.SQLServer: "sqlserver",
}

// Open resolves the dialect and DSN, opens the pool and verifies it with a
// bounded ping. Errors never contain the DSN, which usually holds a password.
func Open(ctx context.Context, db Database) (*sql.DB, string, error) {
	name, dsn, err := Resolve(db)
	if err != nil {
		return nil, "", err
	}
	conn, err := openPool(name, dsn)
	if err != nil {
		if name == dialect.SQLServer {
			return nil, "", errors.New("invalid SQL Server connection configuration")
		}
		if name == dialect.Oracle {
			return nil, "", errors.New("invalid Oracle connection configuration")
		}
		if name == dialect.DB2 {
			return nil, "", errors.New("invalid Db2 connection configuration")
		}
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
		if name == dialect.SQLServer {
			return nil, "", sqlServerConnectError(pctx, err)
		}
		if name == dialect.Oracle {
			return nil, "", oracleConnectError(pctx, err)
		}
		if name == dialect.DB2 {
			return nil, "", db2ConnectError(pctx, err)
		}
		return nil, "", fmt.Errorf("connect to %s database: %w", name, scrub(err, db.Password))
	}
	return conn, name, nil
}

func openPool(name, dsn string) (*sql.DB, error) {
	switch name {
	case dialect.Oracle:
		return openOraclePool(dsn)
	case dialect.DB2:
		return openDB2Pool(dsn)
	default:
		return sql.Open(driverNames[name], dsn)
	}
}

// Resolve returns the dialect name and driver DSN for db. The driver is
// taken from db.Driver or inferred from the DSN (postgres://, mysql://,
// sqlserver://, oracle://, db2://, sqlite://, file:, *.db).
func Resolve(db Database) (string, string, error) {
	d, err := resolveDialect(db)
	if err != nil {
		return "", "", err
	}
	var dsn string
	switch d.Name() {
	case dialect.Postgres:
		dsn, err = postgresDSN(db)
	case dialect.MySQL:
		dsn, err = mysqlDSN(db)
	case dialect.SQLServer:
		dsn, err = sqlServerDSN(db)
	case dialect.Oracle:
		dsn, err = oracleDSN(db)
	case dialect.DB2:
		dsn, err = db2DSN(db)
	default:
		dsn, err = sqliteDSN(db)
	}
	return d.Name(), dsn, err
}

// Dialect returns the dialect name for db from its driver or DSN scheme
// without needing a complete DSN (for database-free commands such as lint).
func Dialect(db Database) (string, error) {
	d, err := resolveDialect(db)
	if err != nil {
		return "", err
	}
	return d.Name(), nil
}

func resolveDialect(db Database) (*dialect.Dialect, error) {
	driver := db.Driver
	if driver == "" {
		driver = inferDriver(db.DSN)
	}
	if driver == "" {
		if db.DSN == "" && db.Name == "" {
			return nil, ErrNoDatabase
		}
		return nil, errors.New("cannot infer the database driver: set database.driver or --driver (postgres, mysql, sqlite, sqlserver, oracle, db2)")
	}
	return dialect.Lookup(driver)
}

func inferDriver(dsn string) string {
	lower := strings.ToLower(dsn)
	switch {
	case strings.HasPrefix(lower, "postgres://"), strings.HasPrefix(lower, "postgresql://"):
		return dialect.Postgres
	case strings.HasPrefix(lower, "mysql://"):
		return dialect.MySQL
	case strings.HasPrefix(lower, "sqlserver://"):
		return dialect.SQLServer
	case strings.HasPrefix(lower, "oracle://"):
		return dialect.Oracle
	case strings.HasPrefix(lower, "db2://"), strings.HasPrefix(lower, "db2s://"):
		return dialect.DB2
	case strings.HasPrefix(lower, "sqlite://"), strings.HasPrefix(lower, "sqlite3://"), strings.HasPrefix(lower, "file:"),
		strings.HasSuffix(lower, ".db"), strings.HasSuffix(lower, ".sqlite"), strings.HasSuffix(lower, ".sqlite3"),
		lower == ":memory:":
		return dialect.SQLite
	}
	return ""
}

func sqlServerDSN(db Database) (string, error) {
	if db.SSLMode != "" {
		return "", errors.New("set SQL Server TLS options in the sqlserver:// URL or database.params")
	}
	u := &url.URL{Scheme: "sqlserver"}
	if db.DSN != "" {
		parsed, err := url.Parse(db.DSN)
		if err != nil || !strings.EqualFold(parsed.Scheme, "sqlserver") || parsed.Hostname() == "" || parsed.Fragment != "" {
			return "", errors.New("invalid sqlserver:// URL")
		}
		u = parsed
		u.Scheme = "sqlserver"
	} else {
		if db.Name == "" {
			return "", ErrNoDatabase
		}
		u.Host = net.JoinHostPort(or(db.Host, defaultHost), strconv.Itoa(orInt(db.Port, defaultSQLServerPort)))
		if db.User != "" {
			u.User = url.UserPassword(db.User, db.Password)
		}
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("invalid sqlserver:// URL parameters")
	}
	q := url.Values{"encrypt": {"true"}, "trustservercertificate": {"false"}}
	if db.Name != "" && db.DSN == "" {
		q.Set("database", db.Name)
	}
	if err := mergeSQLServerParams(q, db.Params, params); err != nil {
		return "", err
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func mergeSQLServerParams(q url.Values, config map[string]string, dsn url.Values) error {
	seen := make(map[string]bool, len(config)+len(dsn))
	for k, values := range dsn {
		k = strings.ToLower(k)
		if len(values) != 1 || seen[k] {
			return errors.New("duplicate SQL Server URL parameter")
		}
		seen[k] = true
		q.Set(k, values[0])
	}
	configured := make(map[string]bool, len(config))
	for k, value := range config {
		k = strings.ToLower(k)
		if configured[k] {
			return errors.New("duplicate SQL Server configuration parameter")
		}
		configured[k] = true
		if !seen[k] {
			q.Set(k, value)
		}
	}
	return nil
}

func sqlServerConnectError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return fmt.Errorf("connect to SQL Server: %w", cause)
	}
	var serverError mssql.Error
	if errors.As(err, &serverError) {
		return fmt.Errorf("connect to SQL Server: server error %d", serverError.Number)
	}
	// Never include a driver diagnostic containing user information, startup
	// parameters, or a URL whose query string may carry additional credentials.
	return errors.New("connect to SQL Server: check server address, database, credentials, and TLS settings")
}

func postgresDSN(db Database) (string, error) {
	if db.DSN != "" {
		return mergePostgresParams(db)
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

// mergePostgresParams adds params/sslmode from the config to an explicit DSN
// (URL or keyword form) without overriding values the DSN already sets.
func mergePostgresParams(db Database) (string, error) {
	extra := map[string]string{}
	for k, v := range db.Params {
		extra[k] = v
	}
	if db.SSLMode != "" {
		extra["sslmode"] = db.SSLMode
	}
	if len(extra) == 0 {
		return db.DSN, nil
	}
	if strings.Contains(db.DSN, "://") {
		u, err := url.Parse(db.DSN)
		if err != nil {
			return "", errors.New("invalid postgres URL") // the parser error can echo the DSN
		}
		q := u.Query()
		for k, v := range extra {
			if !q.Has(k) {
				q.Set(k, v)
			}
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	dsn := db.DSN
	for k, v := range extra {
		if !strings.Contains(dsn, k+"=") {
			dsn += " " + k + "=" + v
		}
	}
	return dsn, nil
}
