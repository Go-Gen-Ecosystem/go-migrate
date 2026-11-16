<!--
@author Admilson B. F. Cossa
-->
# go-migrate

Versioned SQL **and Go** migrations for **PostgreSQL, MySQL/MariaDB and SQLite**:
a library and a single static CLI (`gomigrate`). It brings Flyway's safety model
(checksums, repeatable migrations, baseline, repair) to Go without a JVM, and
makes each migration safe to run from many replicas at once.

[![Go Reference](https://pkg.go.dev/badge/github.com/go-gen-ecosystem/go-migrate.svg)](https://pkg.go.dev/github.com/go-gen-ecosystem/go-migrate)

```bash
go install github.com/go-gen-ecosystem/go-migrate/cmd/gomigrate@latest
```

## Why go-migrate

| | go-migrate | golang-migrate v4 | goose v3 |
|---|---|---|---|
| Each migration + its history row commit atomically (PG/SQLite) | ✅ | ❌ (row written separately) | ✅ |
| Detects edited applied migrations (checksums) | ✅ | ❌ | ❌ |
| Repeatable migrations (`R__views.sql`) | ✅ | ❌ | ❌ |
| Go migrations in the same ordered sequence | ✅ | ❌ | ✅ |
| Out-of-order apply (late-merged branches), opt-in | ✅ | ❌ | ✅ |
| Baseline an existing database | ✅ | ❌ | ❌ |
| Dry run (`--dry-run`, `Plan`) | ✅ | ❌ | ❌ |
| "Nothing to do" is not an error | ✅ | ❌ (`ErrNoChange`) | ✅ |
| Cross-process lock on PG, MySQL **and** SQLite | ✅ | partial | partial |
| Reads golang-migrate (`.up.sql/.down.sql`) and goose (`-- +goose Up`) files | ✅ | — | — |
| CGO-free binary incl. SQLite | ✅ | depends on build | depends on build |

**Memory per 100 migrations** (same SQL, same pure-Go SQLite, `benchmarks/compare`, Linux,
Go 1.26, 6 runs, `benchstat`): go-migrate **596 KiB**, goose 1.14 MiB, golang-migrate
10.66 MiB, with a similar number of allocations. Wall time is dominated by the database
and is within noise between tools.

## Quick start (CLI)

```bash
gomigrate init                                 # writes .gomigrate.yaml and migrations/
gomigrate create add_users                     # migrations/20261116120000_add_users.sql
gomigrate up --dry-run                         # show the plan, change nothing
gomigrate up                                   # apply everything pending
gomigrate status                               # table; --json for scripts
gomigrate down                                 # roll back the last one (--steps N, --to V, --all)
```

A migration file:

```sql
-- +migrate Up
CREATE TABLE users (id INTEGER PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE);

-- +migrate Down
DROP TABLE users;
```

The DSN can come from the config file, `GOMIGRATE_DSN`, `DATABASE_URL` or `--dsn`, so CI
and containers need no config file at all:

```bash
DATABASE_URL=postgres://app:secret@db:5432/app?sslmode=disable gomigrate up
docker build -t gomigrate . && docker run --rm -v "$PWD/migrations:/migrations" -e GOMIGRATE_DSN=... gomigrate up
```

## Library

```go
import (
	"embed"
	"io/fs"

	migrate "github.com/go-gen-ecosystem/go-migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations
var migrations embed.FS

func migrateDB(ctx context.Context, db *sql.DB) error {
	dir, _ := fs.Sub(migrations, "migrations")
	m, err := migrate.New(db, "postgres", migrate.WithFS(dir)) // + migrate.WithLogger(halologLogger)
	if err != nil {
		return err // malformed files and duplicate versions fail here, not mid-run
	}
	_, err = m.Up(ctx)
	return err
}
```

`Migrator` methods: `Up`, `UpTo`, `Down`, `DownTo`, `Redo`, `Plan`, `PlanDown`, `PlanDownTo`,
`Status`, `Validate`, `Repair`, `MarkApplied`, `Baseline`. Every error is testable with
`errors.Is` (`ErrDirty`, `ErrChecksumMismatch`, `ErrOutOfOrder`, `ErrLockTimeout`, …). Options: `WithFS`, `WithGoMigrations`,
`WithTable`, `WithLockWait`, `WithLogger` ([halolog](https://github.com/go-gen-ecosystem/halolog)),
`WithAllowOutOfOrder`, `WithAllowMissing`, `WithHooks`, `WithInstalledBy`.
A `Migrator` is immutable after `New` and safe for concurrent use.

## Migration files

| File name | Meaning |
|---|---|
| `001_create_users.sql`, `V1.2__add_index.sql` | versioned; `-- +migrate Up` / `Down` markers (or `-- +goose ...`) |
| `20240101120000_seed.up.sql` + `.down.sql` | versioned pair (golang-migrate layout) |
| `R__user_views.sql` | repeatable: re-applied after versioned migrations whenever it changes |
| file without markers | the whole file is the Up section (irreversible) |

- Versions compare **numerically per dotted segment**: `9 < 10`, `1.9 < 1.10`, `001 == 1 == 1.0`.
  Flyway's `V1_2__x.sql` is version `1.2`.
- Duplicate versions, SQL before the first marker, unbalanced blocks and `BEGIN`/`COMMIT`
  inside a transactional migration are load errors.
- Statements split on `;` outside strings, quoted identifiers, comments, PostgreSQL
  `$tag$` bodies and MySQL backticks. Wrap anything else (e.g. SQLite triggers) in
  `-- +migrate StatementBegin` / `StatementEnd`.
- `-- +migrate Up notransaction` runs a migration outside a transaction
  (e.g. `CREATE INDEX CONCURRENTLY`).
- The checksum covers the Up section only and ignores BOMs, CRLF vs LF and trailing
  whitespace, so Windows and Linux checkouts agree and fixing a Down section is not drift.
  The SQL itself is executed exactly as written.

## Safety model

- **Locking.** Changing operations hold a cross-process lock keyed by the schema-qualified
  history table: `pg_advisory_lock` / `GET_LOCK` (released by the server if the process
  dies) and, on SQLite, a lease renewed inside every migration's own transaction. Ten
  replicas can run `up` at boot; each migration runs exactly once. The lock is released on
  Ctrl-C, timeouts and panics.
- **One connection per run.** Every statement of a run executes on the lock's connection,
  so a one-connection pool works, losing the session fails the run instead of continuing
  unprotected, and history writes are schema-qualified: a migration's `SET search_path` or
  `USE` cannot redirect them. Session-lock connections are discarded afterwards, never
  pooled. Migrations within one run share that session: prefer `SET LOCAL`.
- **Atomicity.** On PostgreSQL and SQLite a migration and its history row commit together:
  a failure leaves no trace. MySQL cannot roll back DDL, so the row is written as *dirty*
  first; a failure blocks further runs (`ErrDirty`). Restore the schema by hand, then run
  `gomigrate repair` (you undid the partial change) or `gomigrate mark-applied V` (you
  completed it), instead of silently re-running half-applied SQL.
- **Rollbacks** check the requested count, the baseline and every target's down section
  before running anything, and mark repeatable migrations for re-application.
- **Validation before execution.** Edited applied files (`ErrChecksumMismatch`), applied
  versions missing from the source (`ErrMissingMigration`), pending versions older than the
  latest applied one (`ErrOutOfOrder`), unknown targets and irreversible rollbacks are all
  detected before any SQL runs.
- **Read-only means read-only.** `status`, `validate` and `--dry-run` never create tables.
- **No secrets in output.** DSNs and passwords never appear in errors; failing SQL is
  quoted as a short, one-line excerpt.

## Configuration

`.gomigrate.yaml` (any value may use `${ENV}` or `${ENV:-default}`):

```yaml
database:
  driver: postgres            # postgres | mysql | sqlite (inferred from the DSN if omitted)
  dsn: ${DATABASE_URL}        # or host, port, name, user, password, sslmode, params
  connect_timeout: 10s
migrations:
  dir: migrations
  table: gomigrate_history    # or schema.table
  lock_timeout: 5m
  allow_out_of_order: false
  allow_missing: false
```

Precedence: flags > `GOMIGRATE_DSN`/`GOMIGRATE_DRIVER` > `DATABASE_URL` > file > defaults.
A DSN that overrides the file brings its own driver (`postgres://`, `mysql://`, `sqlite://`).
An explicit `--config` must exist and is never retargeted by `DATABASE_URL`. Prefer the
environment over `--dsn` for credentials: command lines are visible to other processes.

## Commands

| Command | |
|---|---|
| `init` | write a starter config and `migrations/` |
| `create NAME [--seq] [--pair]` | new migration (UTC timestamp or next sequence number) |
| `up [--to V] [--dry-run]` | apply pending migrations |
| `down [--steps N \| --to V \| --all] [--dry-run]` | roll back |
| `redo` | roll back the last migration and apply it again |
| `status [--json] [--fail-on-pending]` | state of every migration; CI gate |
| `validate` | drift, missing, out-of-order and failed migrations |
| `repair` | clear failed rows, accept edited checksums |
| `mark-applied V` | record V as applied without running it (finish a dirty migration) |
| `baseline --version V` | adopt an existing database |

Global: `--config`, `--dsn`, `--driver`, `--dir`, `--table`, `--log-level`, `--log-format text|json`.
Results go to stdout (`status --json` has a stable snake_case schema), logs to stderr.
Exit codes: `0` success, `1` error, `2` pending migrations with `--fail-on-pending`.

## Development

```bash
make check             # lint + tests + govulncheck
make test-race         # race detector (needs cgo)
make test-integration  # conformance suite on real PostgreSQL 17 and MySQL 8.4 (Docker)
make bench             # benchmarks; see also benchmarks/compare
```

Licensed under Apache-2.0.
