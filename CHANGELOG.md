<!--
@author Admilson B. F. Cossa
-->
# Changelog

All notable changes to go-migrate are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.0.0] - 2026-07-07

First public release: a lean, standalone SQL migration engine and CLI for
SQLite, MySQL and PostgreSQL.

### Security
- Bumped `github.com/jackc/pgx/v5` to `v5.9.2`, clearing GO-2026-5004 (SQL
  injection via placeholder confusion). `govulncheck ./...` reports no
  vulnerabilities.
- The database lock manager now validates the configured lock table name against
  a strict SQL-identifier allowlist before interpolating it into lock SQL,
  falling back to the safe default on an invalid name (defense-in-depth against
  config-based SQL injection).
- Credential-bearing config files and migration state/backup files are written
  owner-only (`0600`) in owner-only directories (`0700`).
- Content checksums and deterministic migration IDs now use SHA-256 instead of
  MD5 (change detection only — never a security decision).

### Added
- `gomigrate` CLI (cobra) with `init`, `up`, `down`, `status` and `validate`
  commands and a `--dry-run` flag, wired to the public library API.
- End-to-end CLI acceptance test driving `up → status → down` against a real
  SQLite database and a real `+migrate` file.
- YAML **or** JSON configuration, selected by file extension (`.yaml`/`.yml`
  vs anything else).
- Reusable CI (lint + race tests + coverage gate on Linux & Windows).

### Fixed
- **Migrations are now actually applied.** `MigrationEngine.Migrate` — the
  primary up path behind both the library and the CLI — ran the full pipeline
  (schema table, lock, load, filter, resolve, validate) but had no step that
  executed or recorded migrations, so `up` reported success while leaving the
  database untouched. It now executes and records each pending migration and is
  idempotent on re-run.
- **Zero-timeout lock footgun.** An unconfigured `LockTimeout` (Go zero value)
  made the acquire deadline equal to "now", so migrations failed immediately —
  reliably on Linux/CI — even against an uncontended lock. A zero timeout now
  falls back to a sane bounded wait.
- **Connection leak / SQLite file lock.** The standalone tool's `Close` only
  closed an always-nil field and never closed the migration engine, leaking the
  database connection (and, on Windows, locking the SQLite file) on every run.
- **Postgres foreign-key parsing.** `pg_get_constraintdef` output like
  `REFERENCES users(id)` (no space before the paren) was parsed as a referenced
  table named `users(id)`; the column list is now stripped.
- **YAML/JSON rollback generation.** The versioned auto-rollback generator
  validated YAML/JSON payloads and then re-parsed the same bytes with the XML
  parser, so it always failed; YAML/JSON now round-trip through their own codecs.
- Removed two intermittent test flakes (a coarse-clock `ExecutionTime > 0`
  assertion; a latency threshold that flipped health status under parallel load).

### Changed
- **Lean v1 scope.** Reachability analysis kept the wired SQL core (engine,
  executor, loader, lock, tracker, validator, adapter, standalone, plus the
  reconstructed `logging` and `database` helpers). 21 unwired advanced
  subsystems — drift detection, state-based migrations, changelog formats,
  per-dialect engines, checksum/health/facade/builder/factory — were parked
  under `_parked/` for v1.1 (nothing deleted). `go mod tidy` dropped 11 unused
  dependencies.
- Self-contained module identity: created `go.mod`
  (`github.com/go-gen-ecosystem/go-migrate`) and reconstructed the two core
  packages it was extracted away from (`internal/logging`, `internal/database`).
- Deleted dead builder/factory scaffolding interfaces and the dangling NoSQL
  interface declarations whose implementations were already removed.
- All logging goes through the internal structured logger (no `fmt.Print` in
  library code); `golangci-lint` reports zero issues.
- Removed the four unused, legacy per-database lock managers
  (`mysql`/`postgres`/`sqlite`/`sqlserver`) that duplicated the wired generic
  `DatabaseLockManager`; the engine uses the generic manager for every database.
  They were parked under `_parked/lock/` (nothing deleted). This also removed the
  half-shipped, unreachable SQL Server code and its driver constant — the module
  supports only the databases with a real dialector (SQLite, MySQL, PostgreSQL).
- The reusable `logging` package's `Fatal`/`Fatalf` no longer call `os.Exit`;
  they log at fatal severity and return so library consumers keep control of
  process shutdown and deferred cleanup. Process termination is owned by the CLI.
- The parallel migration executor now bounds concurrency at spawn time, honors
  context cancellation, and waits for all in-flight migrations before returning.

### Testing
- Test coverage raised to ~77% over the shipped surface (from ~13%), driven by
  behavioural tests that run real migrations against in-memory/temp SQLite and
  `go-sqlmock` for Postgres extraction paths. Race-clean.
