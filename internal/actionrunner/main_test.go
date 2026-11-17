// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestActionCommandArguments(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		args          []string
	}{
		{"rollback tag", `down --sql --to-tag "Before release"`, []string{"down", "--sql", "--to-tag", "Before release"}},
		{"mixed quoting", `down --to-tag='Antes da 'versão\ 2`, []string{"down", "--to-tag=Antes da versão 2"}},
		{"escaped quotes", `tag "Team \"A\""`, []string{"tag", `Team "A"`}},
		{"literal backslash", `status --dir "C:\my migrations\db"`, []string{"status", "--dir", `C:\my migrations\db`}},
		{"empty strings", `tag '' ""`, []string{"tag", "", ""}},
		{"literal shell characters", `tag $HOME *.sql ';'`, []string{"tag", "$HOME", "*.sql", ";"}},
		{"line breaks", "tag\n'a\nb' \\\n c", []string{"tag", "a\nb", "c"}},
		{"default command", `lint`, []string{"lint"}},
		{"no args", " \t\n", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			if code := run(tt.command, &out, &stderr); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit %d, stderr %q", code, &stderr)
			}
			var want string
			for _, arg := range tt.args {
				want += arg + "\x00"
			}
			if out.String() != want {
				t.Fatalf("Bash argument records = %q, want %q", &out, want)
			}
		})
	}
}

func TestActionCommandBadQuoting(t *testing.T) {
	for _, command := range []string{`down --to-tag 'Before release`, `down --to-tag "Before release`, `tag ends\`} {
		var out, stderr bytes.Buffer
		if code := run(command, &out, &stderr); code != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), "gomigrate action: command input:") {
			t.Fatalf("exit %d, stdout %q, stderr %q", code, &out, &stderr)
		}
	}
}

// Keep the guard in the default Go suite even on hosts without Bash.
func TestCompositeActionUsesCommandArgumentRecords(t *testing.T) {
	var action struct {
		Outputs map[string]struct{ Value string }
		Runs    struct {
			Steps []struct {
				ID, Run, Shell string
				Env            map[string]string
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &action); err != nil {
		t.Fatal(err)
	}
	if got := action.Outputs["exit-code"].Value; got != "${{ steps.run.outputs.exit-code }}" {
		t.Fatalf("exit-code output = %q; must come from the run step", got)
	}
	for _, step := range action.Runs.Steps {
		if step.ID != "run" {
			continue
		}
		if step.Shell != "bash" || step.Env["INPUT_COMMAND"] != "${{ inputs.command }}" || step.Env["ACTION_PATH"] != "${{ github.action_path }}" {
			t.Fatal("run step must pass the command and action checkout to the Bash runner")
		}
		for _, statement := range []string{
			`go run "$ACTION_PATH/internal/actionrunner/main.go" > "$args_file"`,
			`while IFS= read -r -d '' arg; do args+=("$arg"); done < "$args_file"`,
			`gomigrate "${flags[@]}" "${args[@]}" || code=$?`,
			`printf 'exit-code=%s\n' "$code" >> "$GITHUB_OUTPUT"`,
			`exit "$code"`,
		} {
			if !strings.Contains(step.Run, statement) {
				t.Errorf("run step must include %q", statement)
			}
		}
		return
	}
	t.Fatal("missing composite run step")
}
