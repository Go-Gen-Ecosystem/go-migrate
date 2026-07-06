<!--
@author Admilson B. F. Cossa
-->
# go-migrate

A lightweight, standalone SQL database migration engine and CLI for Go —
a Go-native alternative to Flyway/Liquibase for SQLite, MySQL and PostgreSQL.

[![Go Version](https://img.shields.io/badge/Go-1.26+-blue.svg)](https://go.dev)

`go-migrate` applies, rolls back, validates and reports on versioned SQL
migrations. It ships as both a **library** (embed it in your app) and a **CLI**
(`gomigrate`), driven by a single config file. It has no runtime dependency on
any larger framework.

## Features

- **Versioned SQL migrations** with `up` / `down` sections in a single file.
- **up / down / status / validate** operations, plus dry-run.
- **Migration tracking** in a history table, so re-runs are idempotent.
- **Migration locking** to prevent concurrent runs from colliding.
- **Dependency resolution** and validation before anything is applied.
- **SQLite, MySQL, PostgreSQL** via GORM drivers.
- **YAML or JSON** configuration (chosen by file extension).
- Library **and** CLI from the same core; structured logging throughout.

## Install

```bash
# CLI
go install github.com/go-gen-ecosystem/go-migrate/cmd/gomigrate@latest

# Library
go get github.com/go-gen-ecosystem/go-migrate
```

## Quick start (CLI)

```bash
# 1. Scaffold a config and a migrations/ directory
gomigrate init

# 2. Write a migration: migrations/001_create_users.sql
```

```sql
-- +migrate Up
CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);

-- +migrate Down
DROP TABLE users;
```

```bash
gomigrate up                    # apply all pending migrations
gomigrate status                # show applied / pending / failed counts
gomigrate down --steps 1        # roll back the last migration
gomigrate up --dry-run          # resolve + validate without applying
gomigrate validate              # validate migration files
gomigrate --config db.yaml up   # use a specific config file
```

## Configuration

`gomigrate` reads `.gomigrate.yaml` by default (override with `--config`).
`.yaml`/`.yml` files are parsed as YAML; anything else as JSON.

```yaml
database:
  driver: sqlite            # sqlite | mysql | postgres
  dsn: app.db               # driver DSN (file path, or a MySQL/Postgres DSN)
migration:
  migrations_path: migrations
  table_name: migration_history
  lock_table_name: migration_locks
  lock_timeout: 30s
  stop_on_error: true
  max_rollback_depth: 10
```

## Library usage

```go
import migrations "github.com/go-gen-ecosystem/go-migrate"

tool, err := migrations.NewStandaloneMigrationToolFromPath("db.yaml")
if err != nil {
    return err
}
defer tool.Close()

// "" applies all pending migrations; false = not a dry run.
if err := tool.Migrate(ctx, "", false); err != nil {
    return err
}
status, err := tool.Status(ctx)
```

The lower-level `migrations.NewMigrationTool(config *Config)` is available when
you want to build the config in code rather than load it from a file.

## Migration files

A migration is a single `.sql` file whose name sorts by version, e.g.
`001_create_users.sql` or `20260101120000_add_index.sql`. Use `-- +migrate Up`
and `-- +migrate Down` markers to separate the forward and rollback statements;
the `Down` section powers `gomigrate down`.

## Development

```bash
go build ./...
go test ./... -race          # unit + integration (SQLite in-memory / temp files)
golangci-lint run            # zero issues expected
```

The public library builds standalone (`GOWORK=off go build ./...`). CI runs on
Linux and Windows.

## Scope

v1 ships a focused SQL migration core. Advanced subsystems — schema drift
detection, declarative (state-based) migrations, XML/YAML/JSON changelog formats
with auto-generated rollbacks, per-dialect engines, checksum auditing and a
health subsystem — are built but parked under `_parked/` and slated for v1.1.
See [`_parked/README.md`](_parked/README.md).

## License

See [LICENSE](LICENSE).
