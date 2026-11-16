// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// Command gomigrate applies versioned SQL migrations to PostgreSQL, MySQL and
// SQLite. Run `gomigrate --help` for usage.
//
// Exit codes: 0 success, 1 error, 2 `status --fail-on-pending` found
// pending migrations, 3 `lint` found issues at the --fail-on level (so CI
// can tell "behind" and "risky" apart from "broken").
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-gen-ecosystem/go-migrate/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// Ctrl-C / SIGTERM cancel the context; in-flight transactions roll back
	// and the migration lock is released before exit.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, version, os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv)
	stop()
	os.Exit(code)
}
