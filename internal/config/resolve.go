// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import "strings"

type lookupEnv = func(string) (string, bool)

// A recognized DSN can replace a driver only from a lower priority source.
type source uint8

const (
	fromYAML source = iota
	fromDatabaseURL
	fromEnv
	fromFlag
)

type setting struct {
	raw    string
	source source
}

func (s *setting) override(value string, source source) {
	if value != "" {
		*s = setting{raw: value, source: source}
	}
}

func (s setting) expand(lookup lookupEnv) (string, error) {
	if s.source != fromYAML {
		return s.raw, nil // environment and flag values are already literal
	}
	return ExpandEnv(s.raw, lookup)
}

func databaseSettings(db Database, o Overrides, explicit bool, lookup lookupEnv) (dsn, driver setting) {
	dsn.raw, driver.raw = db.DSN, db.Driver
	if !explicit {
		if v, ok := lookup(EnvDatabaseURL); ok {
			dsn.override(v, fromDatabaseURL)
		}
	}
	if v, ok := lookup(EnvDSN); ok {
		dsn.override(v, fromEnv)
	}
	if v, ok := lookup(EnvDriver); ok {
		driver.override(v, fromEnv)
	}
	dsn.override(o.DSN, fromFlag)
	driver.override(o.Driver, fromFlag)
	return dsn, driver
}

func (cfg *Config) resolve(o Overrides, explicit bool, lookup lookupEnv, use Use) error {
	if use != ForCreate {
		if err := cfg.Database.resolve(o, explicit, lookup, use); err != nil {
			return err
		}
	}
	return cfg.Migrations.resolve(o, lookup, use)
}

func (db *Database) resolve(o Overrides, explicit bool, lookup lookupEnv, use Use) error {
	dsn, driver := databaseSettings(*db, o, explicit, lookup)
	if dsn.source > driver.source {
		// A higher-priority DSN is literal (an environment variable or flag).
		driver.override(inferDriver(dsn.raw), dsn.source)
	}
	name, err := driver.expand(lookup)
	if err != nil {
		return err
	}
	if use == ForLint {
		if name == "" {
			name = dsn.lintDriver(lookup)
		}
		if name == "" {
			return errCannotInferDriver
		}
		*db = Database{Driver: name}
		return nil
	}
	db.Driver = name
	db.DSN, err = dsn.expand(lookup)
	if err != nil {
		return err
	}
	for _, field := range []*string{&db.Host, &db.Name, &db.User, &db.Password, &db.SSLMode} {
		if err := expandString(field, "", lookup); err != nil {
			return err
		}
	}
	return expandMap(&db.Params, nil, lookup)
}

// lintDriver inspects a literal scheme without looking up credentials embedded
// in the DSN. Only try interpolation when the DSN has no literal scheme and
// is the sole source of the dialect.
func (s setting) lintDriver(lookup lookupEnv) string {
	if prefix, _, templated := strings.Cut(s.raw, "${"); s.source == fromYAML && templated {
		if strings.Contains(prefix, "://") || strings.HasPrefix(strings.ToLower(prefix), "file:") {
			return inferDriver(prefix)
		}
	}
	dsn, err := s.expand(lookup)
	if err != nil {
		return ""
	}
	return inferDriver(dsn)
}

func (m *Migrations) resolve(o Overrides, lookup lookupEnv, use Use) error {
	if err := expandString(&m.Dir, o.Dir, lookup); err != nil || use == ForCreate {
		return err
	}
	if err := expandString(&m.Table, o.Table, lookup); err != nil {
		return err
	}
	if use == ForConnection {
		if err := expandString(&m.BaselineOnMigrate, o.BaselineOnMigrate, lookup); err != nil {
			return err
		}
	}
	if err := expandList(&m.Contexts, o.Contexts, lookup); err != nil {
		return err
	}
	if err := expandList(&m.Labels, o.Labels, lookup); err != nil {
		return err
	}
	return expandMap(&m.Placeholders, o.Placeholders, lookup)
}

func expandString(dst *string, override string, lookup lookupEnv) error {
	if override != "" {
		*dst = override
		return nil
	}
	var err error
	*dst, err = ExpandEnv(*dst, lookup)
	return err
}

func expandList(dst *[]string, overrides []string, lookup lookupEnv) error {
	if len(overrides) > 0 {
		*dst = overrides
		return nil
	}
	for i := range *dst {
		if err := expandString(&(*dst)[i], "", lookup); err != nil {
			return err
		}
	}
	return nil
}

func expandMap(dst *map[string]string, overrides map[string]string, lookup lookupEnv) error {
	for k, v := range *dst {
		if _, replaced := overrides[k]; replaced {
			continue
		}
		value, err := ExpandEnv(v, lookup)
		if err != nil {
			return err
		}
		(*dst)[k] = value
	}
	if len(overrides) > 0 && *dst == nil {
		*dst = make(map[string]string, len(overrides))
	}
	for k, v := range overrides {
		(*dst)[k] = v
	}
	return nil
}
