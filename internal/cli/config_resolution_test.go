// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/config"
)

func TestCLIConfigPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, file, wantDB string
		env                map[string]string
		flags              []string
	}{
		{
			name: "flags override YAML and environment", wantDB: "flag-${GM_LITERAL}.db",
			file: `database: {driver: '${GM_MISSING_DRIVER}', dsn: '${GM_MISSING_DSN}'}
migrations:
  dir: ${GM_MISSING_DIR}
  table: ${GM_MISSING_TABLE}
  placeholders: {name: '${GM_MISSING_PLACEHOLDER}'}
`,
			env:   map[string]string{config.EnvDriver: "postgres", config.EnvDSN: "postgres://unused/app"},
			flags: []string{"--driver", "sqlite", "--dsn", "flag-${GM_LITERAL}.db", "--dir", "migrations", "--table", "history", "--placeholder", "name=users_${GM_LITERAL_SQL}"},
		},
		{
			name: "app DSN supplies driver ahead of YAML and ambient URL", wantDB: "app-${GM_LITERAL}.db",
			file: `database: {driver: '${GM_MISSING_DRIVER}', dsn: '${GM_MISSING_DSN}'}
migrations: {dir: '${GM_FIXTURE_DIR}', placeholders: {name: users}}
`,
			env: map[string]string{config.EnvDSN: "sqlite://app-${GM_LITERAL}.db", config.EnvDatabaseURL: "postgres://unused/app", "GM_FIXTURE_DIR": "migrations"},
		},
		{
			name: "ambient URL overrides implicit YAML", wantDB: "ambient.db",
			file: `database: {driver: '${GM_MISSING_DRIVER}', dsn: '${GM_MISSING_DSN}'}
migrations: {placeholders: {name: users}}
`,
			env: map[string]string{config.EnvDatabaseURL: "sqlite://ambient.db"},
		},
		{
			name: "explicit YAML ignores ambient URL and uses injected lookup", wantDB: "selected.db",
			file: `database: {driver: '${GM_FIXTURE_DRIVER}', dsn: '${GM_FIXTURE_DSN}'}
migrations: {placeholders: {name: '${GM_FIXTURE_NAME}'}}
`,
			env: map[string]string{
				config.EnvDatabaseURL: "sqlite://ambient.db", "GM_FIXTURE_DSN": "selected.db",
				"GM_FIXTURE_DRIVER": "sqlite", "GM_FIXTURE_NAME": "users",
			},
			flags: []string{"--config", config.DefaultPath},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.Mkdir("migrations", 0o755); err != nil {
				t.Fatal(err)
			}
			write(t, config.DefaultPath, tc.file)
			write(t, "migrations/1_users.sql", "CREATE TABLE \"${name}\" (id INTEGER);")
			out, code := runCLI(t, tc.env, append(tc.flags, "-o", "json", "up")...)
			var result runJSON
			e := decode(t, out)
			if code != ExitOK || !e.OK || json.Unmarshal(e.Data, &result) != nil || len(result.Applied) != 1 {
				t.Fatalf("up: exit %d, error %+v, applied %d", code, e.Error, len(result.Applied))
			}
			if _, err := os.Stat(tc.wantDB); err != nil {
				t.Fatal("the effective DSN did not select the expected SQLite file")
			}
		})
	}
}

func TestCLIConfigLintDriverPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "migrations/1_index.sql", "CREATE INDEX users_id ON users (id);")
	for _, tc := range []struct {
		name, config string
		env          map[string]string
		flags        []string
	}{
		{"same-level flags", "", nil, []string{"--driver", "postgres", "--dsn", "sqlite://ignored.db"}},
		{"same-level env", "", map[string]string{config.EnvDriver: "postgres", config.EnvDSN: "sqlite://ignored.db"}, nil},
		{"higher-level explicit driver", "", map[string]string{config.EnvDSN: "sqlite://ignored.db"}, []string{"--driver", "postgres"}},
		{"same-level YAML with expanded driver", "database: {driver: '${GM_DIALECT}', dsn: 'sqlite://ignored.db'}", map[string]string{"GM_DIALECT": "postgres"}, nil},
		{"infer from substituted DSN", "database: {dsn: '${GM_LINT_URL}'}", map[string]string{"GM_LINT_URL": "postgres://unused/app"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, config.DefaultPath, tc.config)
			out, code := runCLI(t, tc.env, append(tc.flags, "-o", "json", "lint")...)
			if e := decode(t, out); code != ExitOK || !e.OK || !strings.Contains(string(e.Data), "index-not-concurrent") {
				t.Fatalf("lint did not select PostgreSQL: exit %d, envelope %+v", code, e)
			}
		})
	}
}

func TestOfflineLintConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("changes", 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, "changes/1_index.sql", "CREATE INDEX ${index_name} ON ${migrate:table} (id);")
	const migrationConfig = `
migrations:
  dir: ${GM_FIXTURE_DIR}
  table: ${GM_FIXTURE_TABLE}
  baseline_on_migrate: ${GM_BASELINE_UNUSED_OFFLINE}
  placeholders: {index_name: '${GM_FIXTURE_INDEX}'}
`
	for _, tc := range []struct {
		name, database, wantErr, onlineVar string
	}{
		{"driver with unavailable connection fields", `driver: 'postgres', dsn: '${GM_OFFLINE_DSN}', host: '${GM_HOST}', name: '${GM_NAME}', user: '${GM_USER}', password: '${GM_PASS}', sslmode: '${GM_SSL}', params: {arbitrary_key: '${GM_PARAMETER}'}`, "", "GM_OFFLINE_DSN"},
		{"infer literal scheme without credentials", `dsn: 'postgres://${GM_USER}:${GM_PASS}@unused/app'`, "", "GM_USER"},
		{"unresolved DSN without a driver", `dsn: '${GM_OFFLINE_DSN}'`, "set database.driver or --driver", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, config.DefaultPath, "database: {"+tc.database+"}\n"+migrationConfig)
			env := map[string]string{"GM_FIXTURE_DIR": "changes", "GM_FIXTURE_TABLE": "users", "GM_FIXTURE_INDEX": "users_id"}
			out, _ := runCLI(t, env, "-o", "json", "lint")
			checkLintConfig(t, decode(t, out), tc.wantErr)
			g := mcpGlobals(env)
			g.getenv = func(key string) (string, bool) {
				if key == tc.onlineVar {
					t.Error("offline lint queried for an unused connection input")
				}
				return envFunc(env)(key)
			}
			cs := connectMCP(t, g)
			e, isErr := call(t, cs, "lint", nil)
			if isErr != (tc.wantErr != "") {
				t.Errorf("MCP lint IsError = %t", isErr)
			}
			checkLintConfig(t, e, tc.wantErr)
			if tc.wantErr != "" {
				return
			}

			out, code := runCLI(t, env, "-o", "json", "status")
			if e := decode(t, out); code != ExitError || e.Error == nil ||
				!strings.Contains(e.Error.Message, "environment variable "+tc.onlineVar+" is not set") {
				t.Fatal("online status must reject missing connection inputs")
			}
			delete(env, "GM_FIXTURE_INDEX")
			out, _ = runCLI(t, env, "-o", "json", "lint")
			checkLintConfig(t, decode(t, out), "environment variable GM_FIXTURE_INDEX is not set")
			e, _ = call(t, cs, "lint", nil)
			checkLintConfig(t, e, "environment variable GM_FIXTURE_INDEX is not set")
		})
	}
}

func checkLintConfig(t *testing.T, e testEnvelope, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if e.OK || e.Error == nil || !strings.Contains(e.Error.Message, wantErr) {
			t.Errorf("lint should report %q; got %+v", wantErr, e.Error)
		}
		return
	}
	if !e.OK || !strings.Contains(string(e.Data), "index-not-concurrent") ||
		!strings.Contains(string(e.Data), "CREATE INDEX users_id ON users") {
		t.Errorf("lint should report the substituted PostgreSQL index finding; got %+v / %s", e.Error, e.Data)
	}
}

func TestCLIRejectsUnknownConfigKeys(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, body, want string
	}{
		{"root", "databse: {}\n", "databse"},
		{"database", "database: {ssl_mode: require}\n", "ssl_mode"},
		{"migrations", "migrations: {locktime: 10s}\n", "locktime"},
		{"second document", "database: {}\n---\nmigrations: {}\n", "one YAML document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(t, config.DefaultPath, tc.body)
			out, code := runCLI(t, nil, "--driver", "sqlite", "-o", "json", "lint")
			if e := decode(t, out); code != ExitError || e.Error == nil || !strings.Contains(e.Error.Message, tc.want) {
				t.Fatalf("expected a config error mentioning %q: exit %d, error %+v", tc.want, code, e.Error)
			}
		})
	}
}
