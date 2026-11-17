<!--
@author Admilson B. F. Cossa
-->
# go-migrate

**Database migrations for Go teams, CI pipelines and AI agents.** Versioned SQL and
Go migrations for PostgreSQL, MySQL/MariaDB, SQLite, SQL Server, Oracle Database and IBM Db2 LUW, delivered as a driver-free library
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
| Migration + history row commit atomically (PostgreSQL, SQLite, SQL Server) | ✅ | ❌ | ✅ |
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
| Cross-process lock on every supported database | ✅ | partial | partial |
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
  - read-only: `Plan`, `PlanDown`, `PlanRepair`, `PlanID`, `Statements`, `Callbacks`, `Status`, `Validate`, `Lint`, `Migrations`;
  - history administration: `Repair(ctx, migrate.RepairTarget{Migration: "3", Action: migrate.RepairClearFailed})`, `MarkApplied`, `Baseline`, `Tag`. To accept deliberate checksum drift, use `migrate.RepairAcceptChecksum` for that version. Bare `Repair(ctx)` returns `ErrInvalidArgument` and changes nothing.
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

## SQL Server

Use `sqlserver` as the dialect or a `sqlserver://` URL in `GOMIGRATE_DSN`. A library
caller opens its `*sql.DB` using the `sqlserver` driver registered by
`github.com/microsoft/go-mssqldb`. The CLI includes that driver and remains CGO-free.
Choose the target database with the URL's `database` parameter or the YAML
`database.name` field. The CLI enables TLS and verifies the server certificate by
default. Supply driver TLS options via the URL or `database.params`. A disposable
local fixture with a self-signed certificate can set `trustservercertificate=true`.

Each migration direction runs as a T-SQL batch by default. Put `GO` on its own
line to begin a new batch. A trailing `--` comment is allowed. `GO` inside a
quoted identifier, string, or block comment stays in the SQL. Use separate
batches when a statement such as `CREATE PROCEDURE` must be first in its batch.
`GO N` repetition and SQLCMD commands such as `:r` are outside the supported
script format. `StatementBegin`/`StatementEnd` can also set explicit batch
boundaries. The text dry run prints `GO` between batches and the JSON plan lists
the exact payload of each batch.

SQL Server 2022 is the conformance target. The migration login needs access to
the configured history schema and permission to create and modify history and
application objects there. An unqualified history table uses the login's default
schema. See [the SQL Server example](examples/sqlserver).

## Oracle Database

Use the `oracle` dialect or an `oracle://user:password@host:port/service` URL in
`GOMIGRATE_DSN`. The YAML `database.name` field identifies the Oracle service.
The CLI bundles Oracle's native-Go `oracledb` driver and uses password
authentication. The driver requires Go 1.26.9 and keeps the CLI CGO-free.
Library callers can register and open `oracledb` themselves through `database/sql`.
The driver is distributed under the [Universal Permissive License 1.0](https://github.com/oracle/go-oracledb/blob/v26.4.0/LICENSE.txt).

The CLI selects TCPS on port 2484 by default and requests certificate-chain and
server-identity verification. Set `wallet_location` or `ssl_server_cert_dn` as
needed through the URL or `database.params`. An isolated Oracle Free test instance
can use `protocol=tcp` to connect on port 1521. The conformance fixture uses
password authentication over TCP on a private network. It targets Oracle AI
Database 26ai Free and connects to the `FREEPDB1` service.

A history table with no explicit schema uses the session's initial current
schema. The tool folds ordinary configured identifiers to uppercase as Oracle
does for unquoted names. The migration user needs permission to create and
modify its history and application objects and an `EXECUTE` grant on
`SYS.DBMS_LOCK`. Oracle releases the migration lock when the physical session
ends. Use a driver configuration that fails the run on session loss.

Ordinary SQL uses semicolon separators. Wrap Oracle PL/SQL blocks and stored
routines in `-- +migrate StatementBegin` and `-- +migrate StatementEnd`. Keep the
final `END;` and omit the SQL*Plus slash command. Oracle `q'[...]'` and `nq'[...]'`
literals can contain semicolons and newlines. See [the Oracle example](examples/oracle).

## IBM Db2 LUW

Use the `db2` dialect or a `db2://user:password@host:port/database` URL in
`GOMIGRATE_DSN`. The CLI bundles the independent, MIT-licensed
[go-db2 driver](https://github.com/go-db2/go-db2) and remains CGO-free. Library
callers can register the `db2` driver and supply a `database/sql` pool. The
conformance target is IBM Db2 Community Edition 12.1.5.0 on Linux x86-64
with the default database collation.

The CLI enables certificate-verified TLS and defaults to port 50001. Provide
the instance's TLS listener port and `ssl_ca` if you use a private CA (`tls` and
`ssl_ca` can also be set in `database.params`). An isolated fixture can opt in to
unencrypted TCP with `tls=false&allow_plaintext_auth=true`. The CLI supports
username and password authentication.

Db2 history identifiers use uppercase names for ordinary unquoted identifiers.
An unqualified history table uses the initial current schema. The migration
login needs permission to create and modify its history and application objects
and `EXECUTE` on the `SYSIBMADM.DBMS_LOCK` module. Use Db2 LUW 12.1.3 or newer
with the Db2 native `DBMS_LOCK` module installed. Regular schema migrations run
in a transaction. Use `notransaction` for statements that cannot run in one.
Wrap Db2 SQL PL routines or compound statements in `StatementBegin` and
`StatementEnd`. Keep the semicolons inside the block and omit CLP terminators
such as `@`. See [the Db2 example](examples/db2).

## Safety model

- **Locking:** a cross-process lock scoped to the configured migration history.
  - PostgreSQL and MySQL: `pg_advisory_lock` / `GET_LOCK`, which the server releases if the process dies.
  - SQL Server: a session-owned `sp_getapplock` per database. Distinct history tables in the same database share that lock so aliases of a table under a case-insensitive or accent-insensitive collation cannot bypass it.
  - Oracle: an exclusive `DBMS_LOCK` session lock per schema and history table that survives Oracle DDL commits.
  - Db2: a numeric `SYSIBMADM.DBMS_LOCK` session lock derived from the schema and history table. Db2 keeps the lock across commits and releases it when the session ends. Histories that hash to the same ID serialize.
  - SQLite: a run-lifetime OS lock on `<database>.gomigrate-<history-table>.lock` beside the database file, with a fenced lease row. The containing directory must allow creation of that coordination file, and each migrator must be able to open it. Keep the file in place while any migrator might use it. In-memory SQLite databases coordinate within the process.
  - After a SQLite process exits, the OS lock is released and an abandoned lease can be reclaimed once it expires. Drain older versions before rolling out the SQLite lock change so all running migrators use the same coordination protocol.
  - Ten replicas can run `up` at boot, and each migration still runs exactly once.
- **One connection per run.**
  - All statements run on the lock's connection, so one-connection pools work.
  - If the session is lost, the run fails instead of continuing unprotected.
  - History writes use the schema selected before the run. SQL Server also qualifies them with the starting database, so `SET search_path` or `USE` in a migration cannot redirect history writes.
  - Session-lock connections are discarded afterwards, never returned to the pool.
- **Atomicity.**
  - PostgreSQL, SQLite, SQL Server and Db2 normally commit each migration with its history row. Mark SQL Server statements that require autocommit, such as `CREATE DATABASE`, with `notransaction`.
  - MySQL, Oracle, and migrations marked `notransaction` commit a dirty row first. Oracle DDL commits implicitly. A partial failure blocks further runs. Restore the schema by hand, then use `repair MIGRATION --clear-failed` if you undid the change, or `mark-applied VERSION` if you completed the versioned migration.
- **Validation before execution:** drift, missing and out-of-order migrations, unknown targets, the baseline floor and irreversible rollbacks are all checked before any SQL runs.
- **Approval binds execution to the target:** CLI and MCP plan IDs cover the resolved database, history table, active schema or search path, and the exact plan (including callbacks and any automatic baseline). `--expect-plan` (CLI) and `plan_id` (MCP) check the approval under the migration lock before changing history or running migration SQL. Reissue IDs from older releases. Library callers can opt into the same check with `PlanWithID` or `PlanDownWithID` and `ExpectTargetPlan`; `PlanID` and `ExpectPlan` still check plan content only.
- **Approval scope:** IDs use metadata from the live session and the configured network route. A PostgreSQL restart requires a fresh plan. SQLite binds canonical file paths and ordered attachment names; it cannot detect a file replaced at the same path. Re-plan after replacing a file. In-memory or temporary SQLite targets cannot receive a target-bound ID.
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
- **`apply` and `rollback`:** they require `confirm: true` and the `plan_id` of the plan the user approved. Without them they return the plan. If the resolved database, schema or plan differs, re-plan and get fresh approval. `apply` also refuses while pending migrations have lint errors.
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
| `repair MIGRATION (--clear-failed \| --accept-checksum) [--dry-run]` | preview or change one history row |
| `mark-applied V` | record V as applied without running it |
| `baseline --version V` | adopt an existing database |
| `tag NAME` | tag the current version |
| `mcp` | serve AI agents over the Model Context Protocol |

`repair 42 --clear-failed` removes version 42's failed history row after you
undo its partial changes. For a failed repeatable, pass the label from
`status`, for example `repair 'R__refresh views' --clear-failed`.
`repair 42 --accept-checksum` accepts drift for version 42 only; prefer
restoring the applied file and writing a new migration. Changed repeatables
run again on `up` rather than accepting a checksum through repair.
Add `--dry-run` to preview the selected row and, for checksum changes, compare
its stored and proposed checksums. Repair rechecks the selected row under the
migration lock and updates history in a transaction. It runs no migration SQL.

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

To include SQL Server in the normal suite, set `GOMIGRATE_TEST_SQLSERVER_DSN`
to an isolated test instance with permission to create and drop disposable
databases, then run `go test ./...`. Set `GOMIGRATE_TEST_SQLSERVER_SELF_SIGNED=1`
for a self-signed fixture to also verify the default certificate rejection.
Repository owners can enable the SQL Server Developer Edition CI job by setting
`GOMIGRATE_CI_SQLSERVER_ACCEPT_EULA` to `Y` after accepting Microsoft's license
terms for that CI service.

Set `GOMIGRATE_TEST_ORACLE_DSN` to include Oracle in `go test ./...`. Use an
isolated Oracle Database Free PDB with a fixture administrator that can create
and drop disposable users and grant `EXECUTE` on `SYS.DBMS_LOCK`. Test users
receive only the schema privileges needed by the suite and are dropped after
each case.

Set `GOMIGRATE_TEST_DB2_DSN` to an isolated Db2 fixture administrator URL that
can create and drop temporary schemas and flush the fixture's dynamic package
cache. The fixture needs the `SYSTOOLSPACE` tablespace for
`SYSPROC.ADMIN_DROP_SCHEMA`. Set `GOMIGRATE_TEST_DB2_APP_DSN` to
the low-privilege migration user's URL on the same database. To also run the TLS
rejection checks, use a fixture certificate signed by its own CA and set
`GOMIGRATE_TEST_DB2_TLS_MISMATCH_HOST` to an alias that resolves to the fixture
and is absent from its certificate.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the design. Licensed under Apache-2.0.
