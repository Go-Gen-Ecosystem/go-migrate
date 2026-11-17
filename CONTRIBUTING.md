# Contributing to go-migrate

## Report a bug

Open a [GitHub issue](https://github.com/Go-Gen-Ecosystem/go-migrate/issues/new)
with the go-migrate version or commit, operating system, and database engine and
version. Include a minimal migration, the command you ran, and the expected and
observed results. Redact credentials, connection strings, and customer data.
Follow the [security policy](SECURITY.md) for suspected vulnerabilities.

## Propose a change

Keep each pull request focused on one change. Explain the problem, the
compatibility impact, and the checks you ran. Include a regression in the
default Go suite for behavior fixes and update the documentation when public
behavior or configuration changes.

Use the Go toolchain declared in [go.mod](go.mod). Run the relevant checks from
the repository root. The Makefile sets `GOWORK=off` for both targets.

```sh
make check
make test-race
```

`make check` runs lint, the default Go suite, and `govulncheck`.
`make test-race` runs the race detector and requires cgo.

## Database conformance

Run `make test-integration` for PostgreSQL and MySQL with Docker Compose. For
SQL Server, Oracle Database, and Db2 fixture setup, see
[Development](README.md#development). Use disposable databases for conformance
tests because the suite creates and drops database objects.
