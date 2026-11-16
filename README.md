<!--
@author Admilson B. F. Cossa
-->
# go-migrate

**Database migrations for Go teams, CI pipelines and AI agents.** Versioned SQL and
Go migrations for PostgreSQL, MySQL/MariaDB and SQLite, delivered as a driver-free library
and one static CLI. It has Flyway's safety model and Liquibase's targeting, needs no JVM,
lints migration SQL for production risks, and includes an MCP server so agents can
migrate safely.

[![Go Reference](https://pkg.go.dev/badge/github.com/go-gen-ecosystem/go-migrate.svg)](https://pkg.go.dev/github.com/go-gen-ecosystem/go-migrate)

```bash
go install github.com/go-gen-ecosystem/go-migrate/cmd/gomigrate@latest
```

## Why go-migrate

| | go-migrate | golang-migrate v4 | goose v3 |
|---|---|---|---|
| Migration + history row commit atomically (PG/SQLite) | ✅ | ❌ | ✅ |
| Checksums detect edited applied migrations | ✅ | ❌ | ❌ |
| Repeatable migrations, baseline, repair (Flyway) | ✅ | ❌ | ❌ |
| Contexts, labels, preconditions, tags (Liquibase) | ✅ | ❌ | ❌ |
| Placeholders and SQL callbacks (Flyway) | ✅ | ❌ | ❌ |
| Go migrations in the same ordered sequence | ✅ | ❌ | ✅ |
| Out-of-order apply, opt-in | ✅ | ❌ | ✅ |
| Dry run with the exact SQL | ✅ | ❌ | ❌ |
| Static risk linter (locks, rewrites, data loss) | ✅ | ❌ | ❌ |
| Stable JSON output and error codes for CI and agents | ✅ | ❌ | ❌ |
| MCP server for AI agents | ✅ | ❌ | ❌ |
| Cross-process lock on PG, MySQL **and** SQLite | ✅ | partial | partial |
| Reads golang-migrate and goose file formats | ✅ | — | — |
| CGO-free binary including SQLite | ✅ | depends on build | depends on build |

**Memory per 100 migrations** (same SQL, same pure-Go SQLite, `benchmarks/compare`, Linux,
Go 1.26, 6 runs, `benchstat`): go-migrate **596 KiB**, goose 1.14 MiB, golang-migrate
10.66 MiB, with a similar number of allocations. Wall time is dominated by the database and
is within noise between tools.

## Quick start

```bash
gomigrate init                       # .gomigrate.yaml + migrations/
gomigrate create add_users           # migrations/20261116120000_add_users.sql
gomigrate lint                       # risky SQL? (no database needed)
gomigrate up --sql                   # dry run: the exact SQL that would run, and its plan ID
gomigrate up --expect-plan <id>      # apply exactly the reviewed plan, or fail with plan_changed
gomigrate status                     # table; -o json for scripts
gomigrate down                       # roll back the last one (--steps, --to, --to-tag, --all)
```

```sql
-- +migrate Up
CREATE TABLE users (id INTEGER PRIMARY KEY, email VARCHAR(255) NOT NULL UNIQUE);

-- +migrate Down
DROP TABLE users;
```

## Library

```go
//go:embed migrations
var migrations embed.FS

dir, _ := fs.Sub(migrations, "migrations")
m, err := migrate.New(db, "postgres", migrate.WithFS(dir)) // malformed files fail here, not mid-run
if err != nil {
	return err
}
_, err = m.Up(ctx)
```

- **Migrator methods:**
  - apply and roll back: `Up`, `UpTo`, `Down(ctx, migrate.DownTarget{Steps: 1})` (or `To: "3"`, or `Tag: "v1"`), `Redo`;
  - read-only: `Plan`, `PlanDown`, `PlanID`, `Statements`, `Callbacks`, `Status`, `Validate`, `Lint`, `Migrations`;
  - history administration: `Repair`, `MarkApplied`, `Baseline`, `Tag`.
- **Approved plans:** `m.Up(ctx, migrate.ExpectPlan(id))` runs only if the plan, computed again under the lock, still has the ID that was reviewed; otherwise it fails with `ErrPlanChanged` and runs nothing.
- **Options:**
  - `WithFS`, `WithGoMigrations`, `WithTable`, `WithLockWait`;
  - `WithLogger` ([halolog](https://github.com/go-gen-ecosystem/halolog));
  - `WithPlaceholders`, `WithContexts`, `WithLabels`, `WithBaselineOnMigrate`;
  - `WithAllowOutOfOrder`, `WithAllowMissing`, `WithHooks`, `WithInstalledBy`.
- **Without a database:** `migrate.Lint(dialect, opts...)` analyzes migrations with no connection.
- **Errors:** every error works with `errors.Is`, and `migrate.ErrorCodes(err)` gives stable codes.
- **Concurrency:** a `Migrator` is immutable after `New` and safe for concurrent use.

## Migration files

| File | Meaning |
|---|---|
| `001_users.sql`, `V1.2__index.sql` | versioned; `-- +migrate Up` / `Down` markers (or `-- +goose …`) |
| `2024…_seed.up.sql` + `.down.sql` | versioned pair (golang-migrate layout) |
| `R__views.sql` | repeatable: re-applied whenever it changes, and after rollbacks |
| `beforeMigrate.sql`, `afterEachMigrate__x.sql`, … | SQL callbacks (Flyway names) |

Directives:

```sql
-- +migrate Up notransaction                   -- e.g. CREATE INDEX CONCURRENTLY
-- +migrate Context dev,test                   -- run only with --context dev or test
-- +migrate Labels billing                     -- run only with --label billing
-- +migrate DBMS postgres                      -- run only on PostgreSQL
-- +migrate RunAlways                          -- repeatable: run on every up
-- +migrate Precondition onFail=MARK_RAN expect=0 SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'legacy'
-- +migrate Lint ignore=drop-table             -- accept a reviewed lint finding
-- +migrate StatementBegin / StatementEnd      -- keep a block as one statement
```

Supporting rules:

- **Placeholders:** `${name}` is replaced from `--placeholder name=value` or the `placeholders:` config. Built-ins are `${migrate:dialect}`, `${migrate:table}` and `${migrate:user}`.
- **Versions** compare numerically: `9 < 10`, `1.9 < 1.10`, `001 == 1 == 1.0`.
- **Checksums** cover the Up section and ignore line endings, BOMs and trailing whitespace. The SQL runs exactly as written.
- **Preconditions:** `HALT` fails the run, `MARK_RAN` records the migration without running it, and `CONTINUE` leaves it pending. Plans flag such migrations as `conditional`.
- **Callbacks** appear in `--sql` plans (they run even when nothing is pending), are covered by the plan ID and are linted.

## Safety model

- **Locking:** a cross-process lock keyed by the schema-qualified history table.
  - PostgreSQL and MySQL: `pg_advisory_lock` / `GET_LOCK`, which the server releases if the process dies.
  - SQLite: a run-lifetime OS lock on `<database>.gomigrate-<history-table>.lock` beside the database file, with a fenced lease row. The containing directory must allow creation of that coordination file, and each migrator must be able to open it. Keep the file in place while any migrator might use it. In-memory SQLite databases coordinate within the process.
  - After a SQLite process exits, the OS lock is released and an abandoned lease can be reclaimed once it expires. Drain older versions before rolling out the SQLite lock change so all running migrators use the same coordination protocol.
  - Ten replicas can run `up` at boot, and each migration still runs exactly once.
- **One connection per run.**
  - All statements run on the lock's connection, so one-connection pools work.
  - If the session is lost, the run fails instead of continuing unprotected.
  - History writes are schema-qualified, so `SET search_path` or `USE` in a migration cannot redirect them.
  - Session-lock connections are discarded afterwards, never returned to the pool.
- **Atomicity.**
  - PostgreSQL and SQLite normally commit each migration with its history row.
  - MySQL and migrations marked `notransaction` commit a dirty row first. A partial failure blocks further runs (`dirty`) until you choose `repair` (you undid the change) or `mark-applied` (you finished it).
- **Validation before execution:** drift, missing and out-of-order migrations, unknown targets, the baseline floor and irreversible rollbacks are all checked before any SQL runs.
- **Approval binds execution:** a plan ID hashes the dialect, history table, direction, versions, checksums, exact statements and the SQL callbacks that will run. `--expect-plan` (CLI), `ExpectPlan` (library) and `plan_id` (MCP) refuse to run anything else.
- **Read-only means read-only:** `status`, `validate`, `lint` and dry runs never create tables.
- **No connection secrets in output:** DSNs and passwords never appear in errors or logs. Placeholder values do appear in `--sql` plans and SQL excerpts, as they are part of the SQL; do not put secrets in placeholders.

## Lint

`gomigrate lint` needs no database. Its findings, with the rule IDs:

| Severity | Rule IDs |
|---|---|
| error | `add-not-null-without-default`, `concurrently-in-transaction`, `sqlite-unsupported-alter` |
| warning | `index-not-concurrent`, `column-type-change`, `foreign-key-not-valid`, `check-not-valid`, `set-not-null`, `rename`, `drop-table`, `drop-schema`, `drop-column`, `truncate`, `unbounded-write`, `full-table-rewrite` |
| info | `missing-lock-timeout`, `irreversible` |

How it works:

- Rules know the dialect.
- `ALTER TABLE` is split into its actions, so `ADD a int DEFAULT 0, ADD b int NOT NULL` is judged per column.
- Serial, identity and generated columns count as defaulted; quoted and schema-qualified names are matched.
- They ignore text inside strings and comments.
- They skip tables created in the same migration.
- `-o github` turns findings into pull-request annotations.
- `--fail-on error|warning|info|never` exits with code 3.

## AI agents (MCP)

```bash
claude mcp add gomigrate -- gomigrate mcp
```

- **Read-only tools:** `status`, `validate`, `plan` (with SQL) and `lint`.
- **`create_migration`:** writes the file, then validates and lints it.
- **`apply` and `rollback`:** they require `confirm: true` and the `plan_id` of the plan the user approved. Without them they return the exact plan; if anything changed since approval they fail with `plan_changed`. `apply` also refuses while pending migrations have lint errors.
- **`create_migration`** takes typed inputs (`no_transaction`, `sequential`) and rejects `-- +migrate` directives in the SQL, so an agent cannot suppress lint or retarget a migration.

See [AGENTS.md](AGENTS.md) for the workflow and error codes.

## CI/CD

```yaml
# Pull requests: annotate risky SQL, fail on errors
- uses: go-gen-ecosystem/go-migrate@v1
  with: { command: lint, driver: postgres }

# Deploy: show the plan, then apply exactly that plan
- uses: go-gen-ecosystem/go-migrate@v1
  with: { command: up --sql, dsn: "${{ secrets.DATABASE_URL }}" }
- uses: go-gen-ecosystem/go-migrate@v1
  with: { command: "up --expect-plan ${{ vars.APPROVED_PLAN_ID }}", dsn: "${{ secrets.DATABASE_URL }}" }
```

- **JSON envelope:** every command accepts `-o json` and prints one envelope, `{ok, command, data | error{code, codes, message}}`. A run that fails part-way still reports what it applied in `data`.
- **Exit codes:**

| Code | Meaning |
|---|---|
| `0` | ok |
| `1` | error |
| `2` | pending migrations (`status --fail-on-pending`) |
| `3` | lint findings at the `--fail-on` level |

- **Container image:** `docker build -t gomigrate .` produces a 27 MB distroless image.

## Configuration

`.gomigrate.yaml`, where values may use `${ENV}` or `${ENV:-default}`:

```yaml
database:
  driver: postgres            # inferred from the DSN when omitted
  dsn: ${DATABASE_URL}        # or host, port, name, user, password, sslmode, params
migrations:
  dir: migrations
  table: gomigrate_history    # or schema.table
  lock_timeout: 5m
  contexts: [prod]
  placeholders: { owner: app_owner }
  baseline_on_migrate: ""     # e.g. "5" to adopt an existing schema
  allow_out_of_order: false
  allow_missing: false
```

Settings are applied in this order of precedence:

1. flags;
2. `GOMIGRATE_DSN` / `GOMIGRATE_DRIVER`;
3. `DATABASE_URL`;
4. the config file;
5. defaults.

Additional rules:

- A DSN that overrides the file brings its own driver.
- An explicit `--config` must exist, and `DATABASE_URL` never retargets it.
- Prefer the environment over `--dsn` for credentials, because command lines are visible to other processes.

## Commands

| Command | Purpose |
|---|---|
| `init` | write a config and `migrations/` |
| `create NAME [--seq] [--pair]` | create a migration |
| `lint [--fail-on LEVEL]` | static risk analysis |
| `up [--to V] [--dry-run \| --sql] [--expect-plan ID]` | apply pending migrations |
| `down [--steps N \| --to V \| --to-tag T \| --all] [--dry-run \| --sql] [--expect-plan ID]` | roll back |
| `redo` | roll back and re-apply the last migration |
| `status [--fail-on-pending]` | show state; doubles as a CI gate |
| `validate` | check drift, missing, out-of-order and failed migrations |
| `repair` | clear failed rows and accept edited checksums |
| `mark-applied V` | record V as applied without running it |
| `baseline --version V` | adopt an existing database |
| `tag NAME` | tag the current version |
| `mcp` | serve AI agents over the Model Context Protocol |

Global flags:

| Purpose | Flags |
|---|---|
| Connection | `--config`, `--dsn`, `--driver` |
| Migrations | `--dir`, `--table`, `--context`, `--label`, `--placeholder`, `--baseline-on-migrate` |
| Output | `-o text\|json` |
| Logging | `--log-level`, `--log-format` |

## Development

```bash
make check             # lint + tests + govulncheck
make test-race         # race detector (needs cgo)
make test-integration  # conformance on real PostgreSQL 17 and MySQL 8.4 (Docker)
make bench             # benchmarks; see also benchmarks/compare
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design. Licensed under Apache-2.0.
