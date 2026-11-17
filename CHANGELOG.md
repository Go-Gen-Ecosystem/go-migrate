<!--
@author Admilson B. F. Cossa
-->
# Changelog

All notable changes to go-migrate are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## [1.0.1] - 2026-11-17

- Removed archived migration implementations and standalone project documents.

## [1.0.0] - 2026-11-17

First public release. The pre-release GORM-based engine (never published) was
replaced by a lean core on `database/sql`; a pre-launch audit found 17 critical
defects in it (non-exclusive locking, MySQL unusable, wrong rollback counts,
temporary SQLite databases, failures reported as success).

### Added
- `migrate` package (driver-free): `New(db, dialect, opts...)` with `Up`,
  `UpTo`, `Down`, `DownTo`, `Redo`, `Plan`, `PlanDown`, `PlanDownTo`,
  `Status`, `Validate`, `Repair`, `MarkApplied`, `Baseline`.
- PostgreSQL, MySQL/MariaDB and SQLite (pure-Go `modernc.org/sqlite` in the
  CLI: CGO-free binaries for linux/darwin/windows × amd64/arm64).
- SQL Server 2022 migration support with Microsoft's pure-Go driver, T-SQL
  batches, database-scoped application locking, and verified TLS by default.
- Oracle AI Database 26ai Free support with Oracle's native Go driver, explicit
  PL/SQL blocks, and migration locks that survive implicit DDL commits. The
  Oracle driver raises the Go module minimum to 1.26.9.
- IBM Db2 LUW support with a pure-Go driver, verified TLS by default,
  transactional schema migrations, SQL PL blocks, and Db2 application locks.
- Migration formats: `001_x.sql` / `V1.2__x.sql` with `-- +migrate` or
  `-- +goose` markers, golang-migrate `.up.sql`/`.down.sql` pairs, Flyway
  `R__x.sql` repeatable migrations, Go migrations, `fs.FS` / `embed.FS`.
- Cross-process locking: PostgreSQL advisory locks, MySQL `GET_LOCK`,
  fenced lease on SQLite; every run executes on the lock's own connection.
- Per-migration transactions with the history row (PostgreSQL, SQLite, SQL Server);
  dirty-row protocol plus `repair` / `mark-applied` where DDL cannot be
  rolled back (MySQL, `notransaction`).
- Checksum drift, missing-migration and out-of-order detection before any SQL
  runs; opt-in `WithAllowOutOfOrder` / `WithAllowMissing`.
- Hooks, halolog structured logging, sentinel errors for `errors.Is`.
- `gomigrate` CLI: `init`, `create`, `up`, `down`, `redo`, `status`
  (`--fail-on-pending` → exit 2), `validate`, `repair`,
  `mark-applied`, `baseline`; YAML config with `${ENV}` expansion;
  distroless container image.
- Flyway/Liquibase features as SQL directives: placeholders, contexts,
  labels, DBMS filters, preconditions (HALT/MARK_RAN/CONTINUE), RunAlways,
  SQL callback files, baselineOnMigrate, tags with `down --to-tag`, and
  `--sql` dry runs printing the exact statements.
- `gomigrate lint` / `migrate.Lint`: database-free, dialect-aware risk
  analysis (17 rules, ALTER TABLE judged per action) with text, JSON and
  GitHub-annotation output.
- Machine contract: `-o json` envelope on every command, stable error codes
  (`migrate.ErrorCodes`), exit codes 0/1/2/3.
- `gomigrate mcp`: Model Context Protocol server for AI agents, with
  `apply`/`rollback` gated on `confirm` plus the approved `plan_id`, a lint
  gate, and directive-free typed migration authoring.
- Plan binding: `PlanID`, `ExpectPlan` and `--expect-plan` re-check the plan
  under the migration lock and fail with `plan_changed` instead of running
  anything the reviewer did not see. The ID covers the dialect, history
  table and SQL callbacks; `Callbacks(dir)` and `--sql` plans show the
  callbacks, and precondition-gated migrations are flagged `conditional`.
- GitHub Action (`action.yml`) for lint annotations, gates and deploys.
- Dialect conformance suite on real PostgreSQL 17 and MySQL 8.4 (CI job),
  fuzzed SQL splitter, benchmarks including a comparison with goose and
  golang-migrate (`benchmarks/compare`).

### Security
- Go toolchain pinned to 1.26.11 and dependencies bumped: govulncheck
  reports no vulnerabilities.
- History table names are validated identifiers; DSNs and passwords never
  appear in errors; config files are written owner-only.
- File directives in `.down.sql` and callback files are rejected
  (`misplaced_directive`) instead of being silently ignored.
- The GitHub Action splits `command` without glob expansion, passes `--dir`
  only when set and installs the version matching the action ref.
