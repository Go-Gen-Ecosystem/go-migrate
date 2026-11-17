// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package dialect

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrApprovalTarget is deliberately free of database, host and driver
// diagnostics: those can contain connection configuration or credentials.
var ErrApprovalTarget = errors.New("cannot identify the resolved database and schema for plan approval")

// The metadata comes from the pinned session, not from a configured database
// or schema name. An explicitly qualified history table does not necessarily
// describe where unqualified migration statements will run.
var approvalQueries = map[string]string{
	Postgres: `SELECT pg_catalog.current_database(), oid::text,
  COALESCE(pg_catalog.inet_server_addr()::text, 'unix'),
  COALESCE(pg_catalog.inet_server_port(), 0)::text,
  EXTRACT(EPOCH FROM pg_catalog.pg_postmaster_start_time())::text,
  pg_catalog.current_schema(), pg_catalog.current_schemas(true)::text,
  pg_catalog.current_setting('search_path')
  FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database()`,
	MySQL: `SELECT DATABASE(), @@hostname, @@port, @@server_id`,
	SQLServer: `SELECT DB_NAME(), SCHEMA_NAME(), CONVERT(nvarchar(128), SERVERPROPERTY('ServerName')),
  COALESCE(CONVERT(nvarchar(36), service_broker_guid), '-')
  FROM sys.databases WHERE database_id = DB_ID()`,
	Oracle: `SELECT SYS_CONTEXT('USERENV', 'DB_UNIQUE_NAME'),
  COALESCE(SYS_CONTEXT('USERENV', 'CON_NAME'), SYS_CONTEXT('USERENV', 'DB_NAME')),
  SYS_CONTEXT('USERENV', 'SERVER_HOST'), SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA'),
  COALESCE(SYS_CONTEXT('USERENV', 'CURRENT_EDITION_NAME'), '-') FROM DUAL`,
	DB2: `SELECT RTRIM(CURRENT SERVER), RTRIM(CURRENT SCHEMA), RTRIM(CURRENT PATH), INST_NAME, HOST_NAME
  FROM SYSIBMADM.ENV_INST_INFO CROSS JOIN SYSIBMADM.ENV_SYS_INFO`,
}

// ApprovalTarget describes the resolved history and SQL namespaces on conn.
// These parts are for hashing only; callers must not include them in output.
func (d *Dialect) ApprovalTarget(ctx context.Context, conn *sql.Conn, table Table) ([]string, error) {
	var fields []string
	var err error
	if d.name == SQLite {
		fields, err = sqliteApprovalTarget(ctx, conn, table.Schema)
	} else {
		fields, err = serverApprovalTarget(ctx, conn, approvalQueries[d.name])
	}
	if err != nil || table.Schema == "" {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrApprovalTarget
	}
	return append([]string{d.name, table.Catalog, table.Schema, table.Name}, fields...), nil
}

func serverApprovalTarget(ctx context.Context, conn *sql.Conn, query string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 || !rows.Next() {
		return nil, ErrApprovalTarget
	}
	fields, dest := make([]string, len(columns)), make([]any, len(columns))
	for i := range fields {
		dest[i] = &fields[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return nil, ErrApprovalTarget
		}
	}
	if rows.Next() {
		return nil, ErrApprovalTarget
	}
	return fields, rows.Err()
}

func sqliteApprovalTarget(ctx context.Context, conn *sql.Conn, schema string) ([]string, error) {
	rows, err := conn.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var fields []string
	var main, history bool
	for rows.Next() {
		var seq int
		var name, path string
		if err := rows.Scan(&seq, &name, &path); err != nil {
			return nil, err
		}
		if path == "" {
			// A private memory or temp database can be replaced silently
			// between plan and execute, even with the same DSN and alias.
			return nil, ErrApprovalTarget
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		main = main || name == "main"
		history = history || (identPart.MatchString(name) && strings.EqualFold(name, schema))
		// Attachment order affects resolution of unqualified names.
		fields = append(fields, strconv.Itoa(seq), name, path)
	}
	if !main || !history {
		return nil, ErrApprovalTarget
	}
	return fields, rows.Err()
}
