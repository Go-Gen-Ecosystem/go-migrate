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
	"bytes"
	"errors"
	"fmt"
	"io"
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

// Use identifies the settings a command needs.
type Use uint8

// Config requirements for each command family.
const (
	ForConnection Use = iota // online migration commands
	ForLint                  // offline SQL analysis, including MCP create
	ForCreate                // CLI create only needs the migration directory
)

// Load reads a config, applies overrides, and expands the YAML values needed
// for use. An implicit missing file uses defaults; an explicit one is an error.
func Load(path string, explicit bool, o Overrides, lookup func(string) (string, bool), use Use) (Config, error) {
	cfg, err := read(path, explicit)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.resolve(o, explicit, lookup, use); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

func read(path string, explicit bool) (Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	loader, err := yaml.NewLoader(bytes.NewReader(raw), yaml.WithKnownFields())
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := loader.Load(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Validate the entire stream instead of silently ignoring later documents.
	if err := loader.Load(new(yaml.Node)); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, fmt.Errorf("parse config %s: %w", path, err)
		}
		return Config{}, fmt.Errorf("parse config %s: expected one YAML document", path)
	}
	return cfg, nil
}

// Overrides are values from flags; empty fields leave the config unchanged.
type Overrides struct {
	DSN, Driver, Dir, Table string
	BaselineOnMigrate       string
	Contexts, Labels        []string
	// Placeholders are merged over the file's, key by key.
	Placeholders map[string]string
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
