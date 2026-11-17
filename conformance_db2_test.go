// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"crypto/x509"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	db2 "github.com/go-db2/go-db2"

	"github.com/go-gen-ecosystem/go-migrate/internal/config"
	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

const db2FixtureEnv = "GOMIGRATE_TEST_DB2_DSN"
const db2AppFixtureEnv = "GOMIGRATE_TEST_DB2_APP_DSN"

func db2Target() target {
	return target{
		dialect: dialect.DB2,
		open:    openDB2,
		failing: "CREATE TABLE half (id INT);\nINSERT INTO nope VALUES (1);",
		special: map[string]string{
			"5_proc.sql": `-- +migrate Up
CREATE TABLE db2_audit (n INT, message VARCHAR(40));
-- +migrate StatementBegin
CREATE PROCEDURE write_audit() LANGUAGE SQL
BEGIN
    INSERT INTO db2_audit VALUES (1, 'it''s;fine');
    INSERT INTO db2_audit VALUES (2, 'a;b');
END;
-- +migrate StatementEnd
CALL write_audit();
-- +migrate Down
DROP PROCEDURE write_audit;
DROP TABLE db2_audit;`,
		},
		specialCheck: "SELECT COUNT(*) FROM db2_audit WHERE message IN ('it''s;fine', 'a;b')",
	}
}

func db2Fixture(t *testing.T) *sql.DB {
	t.Helper()
	base := os.Getenv(db2FixtureEnv)
	if base == "" {
		t.Skip(db2FixtureEnv + " not set")
	}
	db, name, err := config.Open(t.Context(), config.Database{DSN: base})
	if err != nil || name != dialect.DB2 {
		t.Fatalf("open isolated Db2 fixture: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Each fixture connection starts in its own tested schema. The connector
// carries the same application credentials and TLS policy as the CLI path.
type schemaConnector struct {
	driver.Connector
	schema string
}

func (c schemaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	for _, stmt := range []string{"SET SCHEMA " + c.schema, "SET PATH SYSTEM PATH, " + c.schema} {
		if _, err := conn.(driver.ExecerContext).ExecContext(ctx, stmt, nil); err != nil {
			_ = conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

func openDB2(t *testing.T) *sql.DB {
	t.Helper()
	admin := db2Fixture(t)
	admin.SetMaxIdleConns(0)
	_, dsn, err := config.Resolve(config.Database{DSN: os.Getenv(db2AppFixtureEnv)})
	if err != nil {
		t.Fatalf("resolve least-privilege Db2 fixture: %v", err)
	}
	driverConfig, err := db2.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid least-privilege Db2 fixture DSN")
	}
	user, err := dialect.ParseTable(strings.ToUpper(driverConfig.User))
	if err != nil || user.Schema != "" {
		t.Fatal("invalid Db2 fixture user identifier")
	}
	schema := strings.ToUpper(randName("GM_"))
	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema+" AUTHORIZATION "+user.Name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dropDB2FixtureSchema(t, admin, schema) })
	db := sql.OpenDB(schemaConnector{Connector: db2.NewConnector(driverConfig), schema: schema})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func dropDB2FixtureSchema(t *testing.T, admin *sql.DB, schema string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	errTable := schema + "_DROP"
	const catalog = "SELECT COUNT(*) FROM SYSCAT.SCHEMATA WHERE SCHEMANAME = ?"
	for attempt := 0; attempt < 3; attempt++ {
		// Session teardown can still be releasing references to a dynamic
		// lock plan, even after the application pool has closed.
		if _, err := admin.ExecContext(ctx, "FLUSH PACKAGE CACHE DYNAMIC"); err != nil {
			t.Errorf("flush isolated Db2 fixture package cache: %v", err)
			return
		}
		errorSchema, errorTable := "SYSTOOLS", errTable
		if _, err := admin.ExecContext(ctx, "CALL SYSPROC.ADMIN_DROP_SCHEMA(?, NULL, ?, ?)", schema,
			sql.Out{Dest: &errorSchema, In: true}, sql.Out{Dest: &errorTable, In: true}); err != nil {
			t.Errorf("drop isolated Db2 schema %s: %v", schema, err)
			return
		}
		var remains int
		if err := admin.QueryRowContext(ctx, catalog, schema).Scan(&remains); err != nil {
			t.Errorf("verify Db2 schema %s was removed: %v", schema, err)
			return
		}
		if remains == 0 {
			return
		}
		var planErrors int
		diagnostic := "SYSTOOLS." + errTable
		query := "SELECT COUNT(*) FROM " + diagnostic + " HAVING MIN(SQLCODE) = -478 AND MAX(SQLCODE) = -478"
		if err := admin.QueryRowContext(ctx, query).Scan(&planErrors); err != nil {
			t.Errorf("drop isolated Db2 schema %s: inspect %s for errors: %v", schema, diagnostic, err)
			return
		}
		if _, err := admin.ExecContext(ctx, "DROP TABLE "+diagnostic); err != nil {
			t.Errorf("drop Db2 test diagnostics table: %v", err)
			return
		}
		time.Sleep(time.Second)
	}
	t.Errorf("isolated Db2 schema %s is still present", schema)
}

func TestDB2ScriptComposition(t *testing.T) {
	db, err := sql.Open("db2", "db2://unused:unused@localhost/UNUSED?tls=true")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const ordinary = "CREATE TABLE t (n INT)"
	const block = "CREATE PROCEDURE p() LANGUAGE SQL BEGIN INSERT INTO t VALUES (1); INSERT INTO t VALUES (2); END"
	src := ordinary + ";\n-- +migrate StatementBegin\n" + block + ";\n-- +migrate StatementEnd\nCALL p();"
	m, err := New(db, "db2luw", WithFS(files("1_blocks.sql", src)))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, m.Statements(m.Migrations()[0], Up), []string{ordinary, block, "CALL p()"})
	if _, err := New(db, "db2", WithFS(files("1_invalid.sql", block))); !errors.Is(err, ErrSQLPLBlock) {
		t.Fatalf("unwrapped SQL PL = %v", err)
	}
	if _, err := New(db, "db2", WithFS(files("1_tx.sql", "-- +migrate StatementBegin\nBEGIN COMMIT; END;\n-- +migrate StatementEnd"))); !errors.Is(err, ErrTransactionControl) {
		t.Fatalf("SQL PL transaction control = %v", err)
	}
}

func conformDB2Version(t *testing.T) {
	db := db2Fixture(t)
	var version string
	if err := db.QueryRowContext(t.Context(), "SELECT SERVICE_LEVEL FROM TABLE(SYSPROC.ENV_GET_INST_INFO())").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("IBM Db2 service level %s", strings.TrimSpace(version))
}

func conformDB2Lock(t *testing.T) {
	db := openDB2(t)
	d, _ := dialect.Lookup(dialect.DB2)
	ctx := t.Context()
	table, alias := dialect.Table{Name: "History"}, dialect.Table{Name: "history"}
	held, err := d.Lock(ctx, db, table, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release(ctx) })
	tx, err := held.Conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "CREATE TABLE locked_ddl (n INT)"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []time.Duration{0, 150 * time.Millisecond} {
		second, err := d.Lock(ctx, db, alias, wait)
		if second != nil {
			_ = second.Release(ctx)
		}
		if !errors.Is(err, ErrLockTimeout) {
			t.Fatalf("competing Db2 lock after commit = %v, want timeout", err)
		}
	}
	// Discard the physical session without explicitly releasing its lock.
	if err := held.Conn.Raw(func(any) error { return driver.ErrBadConn }); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("discard pinned session = %v", err)
	}
	reacquired, err := d.Lock(ctx, db, alias, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := reacquired.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func conformDB2Session(t *testing.T, tg target) {
	ctx := t.Context()
	db := tg.open(t)
	db.SetMaxOpenConns(1)
	m, err := New(db, "db2", WithFS(files(
		"1_switch.sql", "SET SCHEMA SYSIBM;",
		"2_still_original.sql", "SET CURRENT LOCK TIMEOUT 10;",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if st, err := m.Status(ctx); err != nil || st.Current != "2" || st.Dirty {
		t.Fatalf("history lost after CURRENT SCHEMA change: %+v, %v", st, err)
	}
}

func conformDB2TLS(t *testing.T) {
	base := os.Getenv(db2AppFixtureEnv)
	if base == "" || os.Getenv(db2FixtureEnv) == "" {
		t.Skip("Db2 fixture DSNs not set")
	}
	otherHost := os.Getenv("GOMIGRATE_TEST_DB2_TLS_MISMATCH_HOST")
	if otherHost == "" {
		t.Skip("Db2 TLS mismatch alias not configured")
	}
	for _, tc := range []string{"untrusted CA", "wrong hostname"} {
		t.Run(tc, func(t *testing.T) {
			u, err := url.Parse(base)
			if err != nil {
				t.Fatal("invalid Db2 TLS fixture URL")
			}
			if tc == "wrong hostname" {
				u.Host = net.JoinHostPort(otherHost, u.Port())
			} else {
				q := u.Query()
				q.Del("ssl_ca")
				u.RawQuery = q.Encode()
			}
			db, _, err := config.Open(t.Context(), config.Database{DSN: u.String()})
			if err == nil {
				_ = db.Close()
				t.Fatal("Db2 TLS accepted an invalid identity")
			}
			if password, _ := u.User.Password(); password != "" && strings.Contains(err.Error(), password) {
				t.Fatal("Db2 TLS error disclosed credentials")
			}
			_, dsn, err := config.Resolve(config.Database{DSN: u.String()})
			if err != nil {
				t.Fatal("invalid Db2 TLS fixture options")
			}
			raw, err := sql.Open("db2", dsn)
			if err != nil {
				t.Fatal("open Db2 TLS fixture pool")
			}
			defer func() { _ = raw.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			err = raw.PingContext(ctx)
			var wrongName x509.HostnameError
			var badCA x509.UnknownAuthorityError
			if (tc == "wrong hostname" && !errors.As(err, &wrongName)) ||
				(tc == "untrusted CA" && !errors.As(err, &badCA)) {
				t.Fatalf("expected Db2 certificate verification error, got %T", err)
			}
		})
	}
}
