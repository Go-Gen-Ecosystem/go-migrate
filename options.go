package migrate

import (
	"io/fs"
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

// WithLockWait sets how long a run waits for the migration lock.
func WithLockWait(d time.Duration) Option { return func(o *options) { o.lockWait = d } }

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
