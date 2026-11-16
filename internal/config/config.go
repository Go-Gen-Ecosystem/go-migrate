// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// Package config loads the gomigrate CLI configuration and opens the
// database it describes.
//
// Precedence, highest first: command-line flags, environment variables
// (GOMIGRATE_DSN, GOMIGRATE_DRIVER, DATABASE_URL), the config file, defaults.
// The file is YAML (JSON is valid YAML) and may reference the environment
// with ${VAR} or ${VAR:-default}, so credentials never need to be committed.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// DefaultPath is the config file used when --config is not given.
const DefaultPath = ".gomigrate.yaml"

// Environment variables consulted when the corresponding flag is unset.
const (
	EnvDSN         = "GOMIGRATE_DSN"
	EnvDriver      = "GOMIGRATE_DRIVER"
	EnvDatabaseURL = "DATABASE_URL"
)

// ErrNoDatabase means no DSN or database name was configured anywhere.
var ErrNoDatabase = errors.New("no database configured: set database.dsn (or host/name) in the config, GOMIGRATE_DSN, DATABASE_URL, or --dsn")

// Config is the CLI configuration.
type Config struct {
	Database   Database   `yaml:"database"`
	Migrations Migrations `yaml:"migrations"`
}

// Database describes the connection. Either DSN, or Name plus the optional
// host/port/credential fields, must be set.
type Database struct {
	Driver         string            `yaml:"driver"`
	DSN            string            `yaml:"dsn"`
	Host           string            `yaml:"host"`
	Port           int               `yaml:"port"`
	Name           string            `yaml:"name"`
	User           string            `yaml:"user"`
	Password       string            `yaml:"password"`
	SSLMode        string            `yaml:"sslmode"`
	Params         map[string]string `yaml:"params"`
	MaxOpenConns   int               `yaml:"max_open_conns"`
	ConnectTimeout time.Duration     `yaml:"connect_timeout"`
}

// Migrations configures the migration run.
type Migrations struct {
	Dir             string        `yaml:"dir"`
	Table           string        `yaml:"table"`
	LockTimeout     time.Duration `yaml:"lock_timeout"`
	AllowOutOfOrder bool          `yaml:"allow_out_of_order"`
	AllowMissing    bool          `yaml:"allow_missing"`
	// Contexts and Labels select Liquibase-style targeted migrations.
	Contexts []string `yaml:"contexts"`
	Labels   []string `yaml:"labels"`
	// Placeholders enables ${name} substitution in SQL (Flyway placeholders).
	Placeholders map[string]string `yaml:"placeholders"`
	// BaselineOnMigrate baselines an existing schema at this version on up.
	BaselineOnMigrate string `yaml:"baseline_on_migrate"`
}

// Default returns the configuration used when nothing else is set.
func Default() Config {
	return Config{Migrations: Migrations{Dir: "migrations"}}
}

// Load reads the config file at path. When explicit is false and the file
// does not exist, the defaults are returned (so a DSN can come purely from
// flags or the environment); when explicit is true a missing file is an
// error, so a typo in --config never silently targets another database.
func Load(path string, explicit bool) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Expand ${VAR} after parsing, inside string values only: a secret such
	// as "*abc" or "a: b" can then never change the YAML structure.
	if err := cfg.expand(os.LookupEnv); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

func (cfg *Config) expand(lookup func(string) (string, bool)) error {
	db, mg := &cfg.Database, &cfg.Migrations
	fields := []*string{&db.Driver, &db.DSN, &db.Host, &db.Name, &db.User, &db.Password, &db.SSLMode, &mg.Dir, &mg.Table}
	for _, f := range fields {
		v, err := ExpandEnv(*f, lookup)
		if err != nil {
			return err
		}
		*f = v
	}
	for k, v := range db.Params {
		ev, err := ExpandEnv(v, lookup)
		if err != nil {
			return err
		}
		db.Params[k] = ev
	}
	for k, v := range mg.Placeholders {
		ev, err := ExpandEnv(v, lookup)
		if err != nil {
			return err
		}
		mg.Placeholders[k] = ev
	}
	return nil
}

// Overrides are values from flags; empty fields leave the config unchanged.
type Overrides struct {
	DSN, Driver, Dir, Table string
	BaselineOnMigrate       string
	Contexts, Labels        []string
	// Placeholders are merged over the file's, key by key.
	Placeholders map[string]string
}

// Apply layers environment variables and then flags over cfg, in increasing
// precedence: DATABASE_URL, GOMIGRATE_DSN / GOMIGRATE_DRIVER, flags.
//
// The generic DATABASE_URL is ignored when the config file was named
// explicitly (--config), so an ambient variable never retargets a run aimed
// at a specific database. When a DSN overrides the file and its scheme
// identifies a driver (postgres://, mysql://, sqlite://, sqlserver://), that driver wins
// over any driver set at a lower level.
func (cfg *Config) Apply(o Overrides, explicitConfig bool, lookup func(string) (string, bool)) {
	const (
		fromFile = iota
		fromDatabaseURL
		fromEnv
		fromFlag
	)
	dsnLevel, driverLevel := fromFile, fromFile
	set := func(dst *string, v string, level int, track *int) {
		if v != "" {
			*dst = v
			if track != nil {
				*track = level
			}
		}
	}
	if v, ok := lookup(EnvDatabaseURL); ok && !explicitConfig {
		set(&cfg.Database.DSN, v, fromDatabaseURL, &dsnLevel)
	}
	if v, ok := lookup(EnvDSN); ok {
		set(&cfg.Database.DSN, v, fromEnv, &dsnLevel)
	}
	if v, ok := lookup(EnvDriver); ok {
		set(&cfg.Database.Driver, v, fromEnv, &driverLevel)
	}
	set(&cfg.Database.DSN, o.DSN, fromFlag, &dsnLevel)
	set(&cfg.Database.Driver, o.Driver, fromFlag, &driverLevel)
	set(&cfg.Migrations.Dir, o.Dir, fromFlag, nil)
	set(&cfg.Migrations.Table, o.Table, fromFlag, nil)
	set(&cfg.Migrations.BaselineOnMigrate, o.BaselineOnMigrate, fromFlag, nil)
	if len(o.Contexts) > 0 {
		cfg.Migrations.Contexts = o.Contexts
	}
	if len(o.Labels) > 0 {
		cfg.Migrations.Labels = o.Labels
	}
	if len(o.Placeholders) > 0 {
		if cfg.Migrations.Placeholders == nil {
			cfg.Migrations.Placeholders = map[string]string{}
		}
		for k, v := range o.Placeholders {
			cfg.Migrations.Placeholders[k] = v
		}
	}

	if dsnLevel > driverLevel {
		if d := inferDriver(cfg.Database.DSN); d != "" {
			cfg.Database.Driver = d
		}
	}
}

// ExpandEnv replaces ${VAR} and ${VAR:-default} in s. A bare '$' is never
// expanded, so values containing '$' survive. An unset variable without a
// default is an error rather than a silent empty string.
func ExpandEnv(s string, lookup func(string) (string, bool)) (string, error) {
	var b strings.Builder
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			b.WriteString(s)
			return b.String(), nil
		}
		j := strings.IndexByte(s[i:], '}')
		if j < 0 {
			return "", errors.New("unterminated ${ in config")
		}
		b.WriteString(s[:i])
		name, def, hasDef := strings.Cut(s[i+2:i+j], ":-")
		v, ok := lookup(name)
		switch {
		case ok && v != "":
			b.WriteString(v)
		case hasDef:
			b.WriteString(def)
		case ok:
		default:
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		s = s[i+j+1:]
	}
}

// Template is the starter config written by `gomigrate init`.
const Template = `# gomigrate configuration. Values may use ${ENV_VAR} or ${ENV_VAR:-default}.
# Flags (--dsn, --driver, --dir, --table) and GOMIGRATE_DSN / DATABASE_URL
# override this file.
database:
  driver: %s
  dsn: %s
  # Instead of dsn you can set: host, port, name, user, password, sslmode, params
  # connect_timeout: 10s
migrations:
  dir: %s
  table: gomigrate_history
  lock_timeout: 5m
  allow_out_of_order: false
  allow_missing: false
`
