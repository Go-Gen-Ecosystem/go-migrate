// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

const (
	pgTestUser          = "gomigrate-private-login"
	pgTestUserEncoded   = "%67omigrate-private-login"
	pgTestPassword      = "gomigrate-private-p@/+:"
	pgTestPasswordURL   = "gomigrate-private-p%40%2F%2B%3A"
	pgTestDB            = "gomigrate-private-database"
	pgTestOption        = "gomigrate-private-option"
	pgTestServerMessage = "gomigrate-private-server-diagnostic"
	pgTestTimeout       = 5 * time.Second
)

func assertPostgresPrivate(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{pgTestUser, pgTestUserEncoded, pgTestPassword, pgTestPasswordURL, pgTestDB, pgTestOption, pgTestServerMessage} {
		if strings.Contains(text, secret) {
			t.Fatal("PostgreSQL connection error disclosed input or server text")
		}
	}
}

func assertPostgresError(t *testing.T, err error, diagnostic string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected PostgreSQL connection error")
	}
	assertPostgresPrivate(t, fmt.Sprintf("%v\n%+v\n%#v", err, err, err))
	var parseErr *pgconn.ParseConfigError
	var connectErr *pgconn.ConnectError
	var serverErr *pgconn.PgError
	if errors.As(err, &parseErr) || errors.As(err, &connectErr) || errors.As(err, &serverErr) {
		t.Fatal("PostgreSQL connection error retained an unsafe pgx cause")
	}
	if !strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("missing safe connection diagnostic: %s", diagnostic)
	}
}

func pgTestURL(addr string) string {
	return "postgres://" + pgTestUserEncoded + ":" + pgTestPasswordURL + "@" + addr + "/" + pgTestDB + "?sslmode=disable"
}

func TestPostgresConfigErrorsKeepCredentialsPrivate(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	invalidURL := config.Database{DSN: pgTestURL("[127.0.0.1") + "&sslpassword=" + pgTestOption}
	serviceFile := filepath.Join(t.TempDir(), "pg_service.conf")
	if err := os.WriteFile(serviceFile, []byte("[gomigrate-redaction]\nuser="+pgTestUser+"\npassword="+pgTestPassword+"\nsslmode="+pgTestOption+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		db   config.Database
		env  map[string]string
	}{
		{"malformed URL", invalidURL, nil},
		{"malformed keyword DSN", config.Database{Driver: "pgx", DSN: "host=127.0.0.1 sslmode=disable user=" + pgTestUser + " password='" + pgTestPassword}, nil},
		{"malformed URL during option merge", config.Database{DSN: invalidURL.DSN, SSLMode: "disable"}, nil},
		{"pgx-only option", config.Database{DSN: pgTestURL("127.0.0.1") + "&default_query_exec_mode=" + pgTestOption}, nil},
		{"structured values", config.Database{Driver: "postgres", Name: pgTestDB, User: pgTestUser, Password: pgTestPassword,
			Params: map[string]string{"port": pgTestOption}}, nil},
		{"pgx environment", config.Database{DSN: "postgres://127.0.0.1/" + pgTestDB}, map[string]string{"PGPASSWORD": pgTestPassword, "PGSSLMODE": pgTestOption}},
		{"pgx service file", config.Database{Driver: "postgres", DSN: "host=127.0.0.1 service=gomigrate-redaction"}, map[string]string{"PGSERVICEFILE": serviceFile}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			conn, _, err := config.Open(t.Context(), tt.db)
			if conn != nil {
				_ = conn.Close()
			}
			assertPostgresError(t, err, "invalid PostgreSQL connection configuration")
			conn, _, _, err = config.OpenWithEndpoint(t.Context(), tt.db)
			if conn != nil {
				_ = conn.Close()
			}
			assertPostgresError(t, err, "invalid PostgreSQL connection configuration")
		})
	}
}

func TestPostgresConnectionOutputKeepsCredentialsPrivate(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "json", "mcp"} {
		t.Run(format, func(t *testing.T) {
			cases := []struct {
				name, dsn, diagnostic string
				serve                 func(*pgproto3.Backend) error
			}{
				{"malformed URL", pgTestURL("[127.0.0.1") + "&sslpassword=" + pgTestOption, "invalid PostgreSQL connection configuration", nil},
				{"malformed keyword DSN", "host=127.0.0.1 sslmode=disable user=" + pgTestUser + " password='" + pgTestPassword, "invalid PostgreSQL connection configuration", nil},
				{"authentication", "", "authentication failed", refusePostgresPassword},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					dsn := tt.dsn
					if tt.serve != nil {
						dsn = pgTestURL(servePostgres(t, tt.serve))
					}
					env := map[string]string{"GOMIGRATE_DRIVER": "postgres", "GOMIGRATE_DSN": dsn}
					var e testEnvelope
					if format == "mcp" {
						g := mcpGlobals(env)
						res, err := connectMCP(t, g).CallTool(t.Context(), &mcp.CallToolParams{Name: "status"})
						if err != nil || !res.IsError {
							t.Fatal("expected status tool error envelope")
						}
						assertPostgresPrivate(t, g.stderr.(*bytes.Buffer).String())
						for _, item := range res.Content {
							if content, ok := item.(*mcp.TextContent); ok {
								assertPostgresPrivate(t, content.Text)
							}
						}
						// Check serialized output before any decoder could print it.
						var raw bytes.Buffer
						if err := encode(&raw, res.StructuredContent); err != nil {
							t.Fatal("failed to encode MCP response")
						}
						assertPostgresPrivate(t, raw.String())
						e = decode(t, raw.String())
					} else {
						var out, stderr bytes.Buffer
						if code := Run(t.Context(), "test", []string{"status", "--output", format}, &out, &stderr, envFunc(env)); code != ExitError {
							t.Fatalf("status exit code = %d", code)
						}
						assertPostgresPrivate(t, out.String())
						assertPostgresPrivate(t, stderr.String())
						if format == "text" {
							if out.Len() != 0 || !strings.Contains(stderr.String(), tt.diagnostic) {
								t.Fatalf("expected safe stderr diagnostic: %s", tt.diagnostic)
							}
							return
						}
						e = decode(t, out.String())
					}
					if e.OK || e.Command != "status" || e.Error == nil || e.Error.Code != "error" ||
						!slices.Equal(e.Error.Codes, []string{"error"}) || !strings.Contains(e.Error.Message, tt.diagnostic) {
						t.Fatalf("expected status error envelope and safe diagnostic: %s", tt.diagnostic)
					}
				})
			}
		})
	}
}

func TestPostgresStartupErrorsKeepCredentialsPrivate(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	for _, tt := range []struct{ code, diagnostic string }{
		{"3D000", "database does not exist"},
		{pgTestOption, "server rejected the connection"},
	} {
		addr := servePostgres(t, func(b *pgproto3.Backend) error {
			b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: tt.code, Message: pgTestServerMessage})
			return b.Flush()
		})
		_, _, err := config.Open(t.Context(), config.Database{DSN: pgTestURL(addr), ConnectTimeout: pgTestTimeout})
		assertPostgresError(t, err, tt.diagnostic)
	}
}

func TestPostgresPoolReconnectKeepsCredentialsPrivate(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	addr := servePostgres(t, acceptPostgresPing, refusePostgresPassword)
	pool, _, endpoint, err := config.OpenWithEndpoint(t.Context(), config.Database{DSN: pgTestURL(addr), ConnectTimeout: pgTestTimeout})
	if err != nil {
		t.Fatal("could not open the test PostgreSQL pool")
	}
	t.Cleanup(func() { _ = pool.Close() })
	if want := fmt.Sprintf("[%q]", addr); endpoint != want {
		t.Fatal("PostgreSQL approval route no longer reflects the connected endpoint")
	}
	pool.SetMaxIdleConns(0)
	ctx, cancel := context.WithTimeout(t.Context(), pgTestTimeout)
	defer cancel()
	assertPostgresError(t, pool.PingContext(ctx), "authentication failed")
}

func TestPostgresDialAndContextErrorsKeepCredentialsPrivate(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("could not reserve a loopback port for the failed dial")
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	db := config.Database{DSN: pgTestURL(addr), ConnectTimeout: pgTestTimeout}
	_, _, err = config.Open(t.Context(), db)
	assertPostgresError(t, err, "check server address")

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New(pgTestPassword))
	_, _, err = config.Open(ctx, db)
	assertPostgresError(t, err, "context canceled")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("connection error must preserve context.Canceled")
	}
	ctx, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	_, _, err = config.Open(ctx, db)
	assertPostgresError(t, err, "context deadline exceeded")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("connection error must preserve context.DeadlineExceeded")
	}
}

// servePostgres speaks the real startup protocol on an owned loopback socket.
// Each handler is required to run, so a parse or dial failure cannot stand in
// for an auth or reconnect regression.
func servePostgres(t *testing.T, handlers ...func(*pgproto3.Backend) error) string {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal("could not start loopback PostgreSQL fixture")
	}
	done := make(chan error, 1)
	go func() {
		for _, handle := range handlers {
			if err := servePostgresConn(listener, handle); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		if err := <-done; err != nil {
			t.Error("loopback PostgreSQL fixture did not complete the expected exchange")
		}
	})
	return listener.Addr().String()
}

func servePostgresConn(listener *net.TCPListener, handle func(*pgproto3.Backend) error) error {
	if err := listener.SetDeadline(time.Now().Add(pgTestTimeout)); err != nil {
		return err
	}
	conn, err := listener.AcceptTCP()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(pgTestTimeout)); err != nil {
		return err
	}
	b := pgproto3.NewBackend(conn, conn)
	msg, err := b.ReceiveStartupMessage()
	if err != nil {
		return err
	}
	if startup, ok := msg.(*pgproto3.StartupMessage); !ok || startup.Parameters["user"] != pgTestUser || startup.Parameters["database"] != pgTestDB {
		return errors.New("unexpected PostgreSQL startup message")
	}
	return handle(b)
}

func refusePostgresPassword(b *pgproto3.Backend) error {
	if err := b.SetAuthType(pgproto3.AuthTypeCleartextPassword); err != nil {
		return err
	}
	b.Send(&pgproto3.AuthenticationCleartextPassword{})
	if err := b.Flush(); err != nil {
		return err
	}
	msg, err := b.Receive()
	if err != nil {
		return err
	}
	if password, ok := msg.(*pgproto3.PasswordMessage); !ok || password.Password != pgTestPassword {
		return errors.New("unexpected PostgreSQL password message")
	}
	b.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: pgTestServerMessage + " " + pgTestPassword + " " + pgTestPasswordURL})
	return b.Flush()
}

func acceptPostgresPing(b *pgproto3.Backend) error {
	b.Send(&pgproto3.AuthenticationOk{})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := b.Flush(); err != nil {
		return err
	}
	msg, err := b.Receive()
	if err != nil {
		return err
	}
	if _, ok := msg.(*pgproto3.Query); !ok {
		return errors.New("expected PostgreSQL ping query")
	}
	b.Send(&pgproto3.EmptyQueryResponse{})
	b.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return b.Flush()
}
