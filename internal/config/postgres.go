// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

var errInvalidPostgresConfig = errors.New("invalid PostgreSQL connection configuration: check the DSN and connection options")

func openPostgresPool(dsn string, withEndpoint bool) (*sql.DB, string, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// ParseConfigError exposes the original DSN even when pgx masks its message.
		return nil, "", errInvalidPostgresConfig
	}
	var endpoint string
	if withEndpoint {
		endpoint = postgresEndpoint(&cfg.Config)
	}
	return sql.OpenDB(postgresConnector{stdlib.GetConnector(*cfg)}), endpoint, nil
}

func postgresEndpoint(cfg *pgconn.Config) string {
	fields := []string{net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))}
	for _, fallback := range cfg.Fallbacks {
		route := net.JoinHostPort(fallback.Host, strconv.Itoa(int(fallback.Port)))
		if !slices.Contains(fields, route) {
			fields = append(fields, route)
		}
	}
	return encodeEndpoint(fields)
}

type postgresConnector struct{ driver.Connector }

func (c postgresConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, postgresConnectError(ctx, err)
	}
	return conn, nil
}

// These are the only connection diagnostics allowed to reach callers. pgx's
// ConnectError carries the full configuration, and server text is arbitrary.
type postgresConnectFailure string

func (e postgresConnectFailure) Error() string { return string(e) }

const (
	postgresAuthFailure    postgresConnectFailure = "connect to PostgreSQL: authentication failed; check credentials and server access rules"
	postgresMissingDB      postgresConnectFailure = "connect to PostgreSQL: database does not exist; check the database name"
	postgresStartupFailure postgresConnectFailure = "connect to PostgreSQL: server rejected the connection; check database and startup settings"
	postgresTimeout        postgresConnectFailure = "connect to PostgreSQL: connection timed out; check server availability and network settings"
	postgresDialFailure    postgresConnectFailure = "connect to PostgreSQL: check server address, database, credentials, and TLS settings"
)

func postgresConnectError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() == context.Canceled || errors.Is(err, context.Canceled):
		return fmt.Errorf("connect to PostgreSQL: %w", context.Canceled)
	case ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("connect to PostgreSQL: %w", context.DeadlineExceeded)
	}
	// Ping can receive a diagnostic that the connector has already classified.
	if safe, ok := err.(postgresConnectFailure); ok {
		return safe
	}
	var serverErr *pgconn.PgError
	if errors.As(err, &serverErr) {
		switch serverErr.Code {
		case "28000", "28P01": // invalid authorization and invalid password
			return postgresAuthFailure
		case "3D000": // invalid catalog name
			return postgresMissingDB
		default:
			return postgresStartupFailure
		}
	}
	if pgconn.Timeout(err) {
		return postgresTimeout
	}
	return postgresDialFailure
}
