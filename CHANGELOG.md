<!--
@author Admilson B. F. Cossa
-->
# Changelog

All notable changes to go-migrate are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## [1.0.0] - Unreleased

First public release. The pre-release GORM-based engine (never published) was
replaced by a lean core on `database/sql`; a pre-launch audit found 17 critical
defects in it (non-exclusive locking, MySQL unusable, wrong rollback counts,
temporary SQLite databases, failures reported as success). The old code is
kept under `_parked/v0` for reference.

### Added
- `migrate` package (driver-free): `New(db, dialect, opts...)` with `Up`,
  `UpTo`, `Down`, `DownTo`, `Redo`, `Plan`, `PlanDown`, `PlanDownTo`,
  `Status`, `Validate`, `Repair`, `MarkApplied`, `Baseline`.
- PostgreSQL, MySQL/MariaDB and SQLite (pure-Go `modernc.org/sqlite` in the
  CLI: CGO-free binaries for linux/darwin/windows × amd64/arm64).
- Migration formats: `001_x.sql` / `V1.2__x.sql` with `-- +migrate` or
  `-- +goose` markers, golang-migrate `.up.sql`/`.down.sql` pairs, Flyway
  `R__x.sql` repeatable migrations, Go migrations, `fs.FS` / `embed.FS`.
- Cross-process locking: PostgreSQL advisory locks, MySQL `GET_LOCK`,
  fenced lease on SQLite; every run executes on the lock's own connection.
- Per-migration transactions with the history row (PostgreSQL, SQLite);
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
  analysis (16 rules) with text, JSON and GitHub-annotation output.
- Machine contract: `-o json` envelope on every command, stable error codes
  (`migrate.ErrorCodes`), exit codes 0/1/2/3.
- `gomigrate mcp`: Model Context Protocol server for AI agents, with
  confirmation-gated `apply`/`rollback` and a lint gate; `AGENTS.md`.
- GitHub Action (`action.yml`) for lint annotations, gates and deploys.
- Dialect conformance suite on real PostgreSQL 17 and MySQL 8.4 (CI job),
  fuzzed SQL splitter, benchmarks including a comparison with goose and
  golang-migrate (`benchmarks/compare`).

### Security
- Go toolchain pinned to 1.26.11 and dependencies bumped: govulncheck
  reports no vulnerabilities.
- History table names are validated identifiers; DSNs and passwords never
  appear in errors; config files are written owner-only.
