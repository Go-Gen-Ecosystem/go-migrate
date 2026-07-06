<!--
@author Admilson B. F. Cossa
-->
# Changelog

All notable changes to go-migrate are documented here. This project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

First public release: a lean, standalone SQL migration engine and CLI for
SQLite, MySQL and PostgreSQL.

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

### Testing
- Test coverage raised to ~77% over the shipped surface (from ~13%), driven by
  behavioural tests that run real migrations against in-memory/temp SQLite and
  `go-sqlmock` for Postgres extraction paths. Race-clean.
