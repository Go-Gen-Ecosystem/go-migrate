// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

const createProcessSwitch = "GOMIGRATE_TEST_CREATE_PROCESS"

// TestCreateProcess runs the production CLI entry point in a separate process.
// It waits for EOF on stdin so the parent can release all contenders together.
func TestCreateProcess(t *testing.T) {
	if os.Getenv(createProcessSwitch) != "1" {
		return
	}
	i := slices.Index(os.Args, "--")
	if i < 0 || i+2 >= len(os.Args) {
		t.Fatal("missing create subprocess arguments")
	}
	timeout, err := time.ParseDuration(os.Args[i+1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	if code := Run(ctx, "test", os.Args[i+2:], os.Stdout, os.Stderr, envFunc(nil)); code != ExitOK {
		t.Fatalf("create exited with code %d", code)
	}
}

func createProcess(t *testing.T, dir string, timeout time.Duration, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, exe, append([]string{
		"-test.run=^TestCreateProcess$", "--", timeout.String(), "--dir", dir, "-o", "json",
	}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), createProcessSwitch+"=1")
	return cmd
}

func TestConcurrentCreate(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "sequential_pairs", args: []string{"--seq", "--pair"}},
		{name: "timestamps"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			read, start, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = read.Close() }()

			type worker struct {
				cmd      *exec.Cmd
				out, log bytes.Buffer
			}
			const count = 8
			workers := make([]*worker, 0, count)
			defer func() {
				_ = start.Close()
				for _, w := range workers {
					if w.cmd.ProcessState == nil {
						_ = w.cmd.Wait()
					}
				}
			}()
			for i := range count {
				w := &worker{cmd: createProcess(t, dir, 20*time.Second,
					append([]string{"create", fmt.Sprintf("migration_%d", i)}, tc.args...)...)}
				w.cmd.Stdin, w.cmd.Stdout, w.cmd.Stderr = read, &w.out, &w.log
				if err := w.cmd.Start(); err != nil {
					t.Fatal(err)
				}
				workers = append(workers, w)
			}
			if err := start.Close(); err != nil {
				t.Fatal(err)
			}
			versions := make(map[string]int, count)
			for i, w := range workers {
				if err := w.cmd.Wait(); err != nil {
					t.Fatalf("create migration_%d: %v\n%s\n%s", i, err, &w.out, &w.log)
				}
				var envelope testEnvelope
				if err := json.NewDecoder(&w.out).Decode(&envelope); err != nil || !envelope.OK {
					t.Fatalf("create migration_%d: %v, %+v", i, err, envelope)
				}
				var result struct {
					Version string   `json:"version"`
					Files   []string `json:"files"`
				}
				if err := json.Unmarshal(envelope.Data, &result); err != nil || result.Version == "" {
					t.Fatalf("create migration_%d: %v, %s", i, err, envelope.Data)
				}
				if first, ok := versions[result.Version]; ok {
					t.Fatalf("migration_%d and migration_%d both used version %s", first, i, result.Version)
				}
				versions[result.Version] = i

				base := filepath.Join(dir, fmt.Sprintf("%s_migration_%d", result.Version, i))
				if len(tc.args) > 0 {
					assertCreateFiles(t, result.Files, base, []string{".up.sql", ".down.sql"},
						[]string{"-- +migrate Up\n\n", "\n"})
				} else {
					assertCreateFiles(t, result.Files, base, []string{".sql"},
						[]string{"-- +migrate Up\n\n\n-- +migrate Down\n\n"})
				}
			}
		})
	}
}

func assertCreateFiles(t *testing.T, files []string, base string, extensions, bodies []string) {
	t.Helper()
	if len(files) != len(extensions) {
		t.Fatalf("create wrote %v, want %d files", files, len(extensions))
	}
	for i, ext := range extensions {
		if files[i] != base+ext {
			t.Fatalf("create file = %q, want %q", files[i], base+ext)
		}
		if contents, err := os.ReadFile(files[i]); err != nil || string(contents) != bodies[i] {
			t.Fatalf("read %s: got %q, %v; want %q", files[i], contents, err, bodies[i])
		}
	}
}

func TestCreateRespectsDirectoryLock(t *testing.T) {
	dir := t.TempDir()
	lock := flock.New(filepath.Join(dir, createLockName))
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatalf("hold migration directory lock: %t, %v", ok, err)
	}
	defer func() { _ = lock.Close() }()

	out, err := createProcess(t, dir, 100*time.Millisecond, "create", "contender", "--seq").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "context deadline exceeded") {
		t.Fatalf("create must wait on the other process's lock until canceled: %v\n%s", err, out)
	}
	if files, err := filepath.Glob(filepath.Join(dir, "*.sql")); err != nil || len(files) != 0 {
		t.Fatalf("canceled create left migration files: %v, %v", files, err)
	}
}
