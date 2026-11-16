// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"io/fs"
	"strings"
	"time"

	"github.com/go-gen-ecosystem/halolog"
	"github.com/go-gen-ecosystem/halolog/core"
)

// Defaults.
const (
	// DefaultTable is the default history table. It is deliberately distinct
	// from golang-migrate's schema_migrations so both tools can coexist.
	DefaultTable = "gomigrate_history"
	// DefaultLockWait is how long a run waits for another to finish.
	DefaultLockWait = 5 * time.Minute
)

// Option configures a Migrator.
type Option func(*options)

type options struct {
	fsys         fs.FS
	goMigrations []GoMigration
	table        string
	lockWait     time.Duration
	logger       halolog.Logger
	allowOOO     bool
	allowMissing bool
	hooks        Hooks
	installedBy  string
	placeholders map[string]string
	contexts     []string
	labels       []string
	baselineOnUp string
}

func defaults() options {
	return options{
		table:    DefaultTable,
		lockWait: DefaultLockWait,
		logger:   core.New().Component("go-migrate").Discard().MustBuild(),
	}
}

// WithFS loads SQL migrations from fsys, e.g. os.DirFS("migrations") or an
// embed.FS (use fs.Sub to point at a subdirectory).
func WithFS(fsys fs.FS) Option { return func(o *options) { o.fsys = fsys } }

// WithGoMigrations registers migrations written in Go. Their versions share
// one ordering with SQL migrations and must not collide with them.
func WithGoMigrations(ms ...GoMigration) Option {
	return func(o *options) { o.goMigrations = append(o.goMigrations, ms...) }
}

// WithTable sets the history table ("name" or "schema.name").
func WithTable(name string) Option { return func(o *options) { o.table = name } }

// WithLockWait sets the lock acquisition budget, including reserving and
// setting up the session (default DefaultLockWait). Zero or negative makes
// one attempt using the caller's context and returns ErrLockTimeout if held.
func WithLockWait(d time.Duration) Option { return func(o *options) { o.lockWait = max(d, 0) } }

// WithLogger sets the halolog logger used for progress and diagnostics
// (default: a discarding logger). A nil logger keeps the default.
func WithLogger(l halolog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithAllowOutOfOrder applies pending migrations whose version is lower than
// the latest applied one (useful when feature branches merge late) instead
// of failing with ErrOutOfOrder.
func WithAllowOutOfOrder(allow bool) Option { return func(o *options) { o.allowOOO = allow } }

// WithAllowMissing tolerates applied versions that no longer exist in the
// source instead of failing with ErrMissingMigration.
func WithAllowMissing(allow bool) Option { return func(o *options) { o.allowMissing = allow } }

// WithHooks installs callbacks that run around each migration.
func WithHooks(h Hooks) Option { return func(o *options) { o.hooks = h } }

// WithInstalledBy sets the value recorded in installed_by (default: OS user).
func WithInstalledBy(name string) Option { return func(o *options) { o.installedBy = name } }

// WithPlaceholders enables ${name} substitution in SQL migrations, callbacks
// and preconditions (Flyway placeholders). Built-ins ${migrate:dialect},
// ${migrate:table} and ${migrate:user} are available once enabled. An
// unresolved ${name} is a load error. Checksums are computed before
// substitution, so per-environment values never look like drift.
func WithPlaceholders(values map[string]string) Option {
	return func(o *options) {
		o.placeholders = make(map[string]string, len(values))
		for k, v := range values {
			o.placeholders[k] = v
		}
	}
}

// WithContexts selects the active contexts (Liquibase contexts). A migration
// declaring "-- +migrate Context ..." runs only if one of its contexts is
// active; migrations without contexts always run. Without this option every
// migration runs.
func WithContexts(contexts ...string) Option {
	return func(o *options) { o.contexts = lowerAll(contexts) }
}

// WithLabels selects the active labels, with the same rules as WithContexts
// for migrations declaring "-- +migrate Labels ...".
func WithLabels(labels ...string) Option {
	return func(o *options) { o.labels = lowerAll(labels) }
}

// WithBaselineOnMigrate makes Up baseline at version first when the history
// table is empty but the schema already contains tables (Flyway
// baselineOnMigrate), so an existing database can adopt go-migrate in place.
func WithBaselineOnMigrate(version string) Option {
	return func(o *options) { o.baselineOnUp = version }
}

func lowerAll(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}
