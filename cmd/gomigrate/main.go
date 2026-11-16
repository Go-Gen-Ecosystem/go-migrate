// Command gomigrate applies versioned SQL migrations to PostgreSQL, MySQL and
// SQLite. Run `gomigrate --help` for usage.
//
// Exit codes: 0 success, 1 error, 2 `status --fail-on-pending` found
// pending migrations (so CI can tell "behind" apart from "broken").
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-gen-ecosystem/go-migrate/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	exitError   = 1
	exitPending = 2
)

func main() {
	// Ctrl-C / SIGTERM cancel the context; in-flight transactions roll back
	// and the migration lock is released before exit.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := cli.NewRoot(version).ExecuteContext(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if errors.Is(err, cli.ErrPending) {
			os.Exit(exitPending)
		}
		os.Exit(exitError)
	}
}
