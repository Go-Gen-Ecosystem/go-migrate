// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// Command releasecheck verifies the source and archives used by the release workflow.
package main

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	checksums = "checksums.txt"
	changelog = "CHANGELOG.md"
)

var (
	tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
	shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 3 || !tagPattern.MatchString(args[1]) {
		return errors.New("releasecheck requires source, payload, attest, or smoke and a v-prefixed version tag")
	}
	mode, tag := args[0], args[1]
	version := strings.TrimPrefix(tag, "v")
	switch {
	case mode == "source" && len(args) == 3 && shaPattern.MatchString(args[2]):
		return verifySource(ctx, ".", tag, args[2])
	case mode == "payload" && len(args) == 3:
		return verifyPayload(args[2], version)
	case mode == "attest" && len(args) == 5 && shaPattern.MatchString(args[4]):
		dir, repo, sha := args[2], args[3], args[4]
		if err := verifyPayload(dir, version); err != nil {
			return err
		}
		for _, name := range append(archiveNames(version), checksums, changelog) {
			out, err := command(ctx, "", "gh", "attestation", "verify", filepath.Join(dir, name),
				"--repo", repo, "--signer-workflow", repo+"/.github/workflows/release.yml",
				"--source-ref", "refs/tags/"+tag, "--source-digest", sha, "--deny-self-hosted-runners")
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintln(os.Stdout, out); err != nil {
				return err
			}
		}
		return nil
	case mode == "smoke" && len(args) == 6 && shaPattern.MatchString(args[3]):
		if args[4] != runtime.GOOS || args[5] != runtime.GOARCH {
			return fmt.Errorf("runner is %s/%s, expected native %s/%s", runtime.GOOS, runtime.GOARCH, args[4], args[5])
		}
		dir, sha := args[2], args[3]
		if err := verifyPayload(dir, version); err != nil {
			return err
		}
		archive := filepath.Join(dir, archiveName(version, runtime.GOOS, runtime.GOARCH))
		return smoke(ctx, archive, version, sha)
	default:
		return fmt.Errorf("invalid arguments for releasecheck %s", mode)
	}
}

func command(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	out := strings.TrimSpace(string(output))
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out, fmt.Errorf("%s %s: %w\n%s\n%s", name, strings.Join(args, " "), err, out, exit.Stderr)
		}
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func verifySource(ctx context.Context, dir, tag, sha string) error {
	for _, ref := range []string{"HEAD", "refs/tags/" + tag + "^{commit}"} {
		revision, err := command(ctx, dir, "git", "rev-parse", "--verify", ref)
		if err != nil {
			return err
		}
		if revision != sha {
			return fmt.Errorf("%s resolves to %s, expected %s", ref, revision, sha)
		}
	}
	if _, err := command(ctx, dir, "git", "merge-base", "--is-ancestor", sha, "refs/remotes/origin/main"); err != nil {
		return fmt.Errorf("release commit must be on origin/main: %w", err)
	}
	status, err := command(ctx, dir, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("release source must be clean:\n%s", status)
	}
	return nil
}

func archiveName(version, goos, goarch string) string {
	suffix := ".tar.gz"
	if goos == "windows" {
		suffix = ".zip"
	}
	return "go-migrate_" + version + "_" + goos + "_" + goarch + suffix
}

func archiveNames(version string) []string {
	names := make([]string, 0, 6)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			names = append(names, archiveName(version, goos, goarch))
		}
	}
	return names
}

func verifyPayload(dir, version string) error {
	archives := archiveNames(version)
	expected := append(slices.Clone(archives), checksums, changelog)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("expected %d release files, got %d", len(expected), len(entries))
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !slices.Contains(expected, entry.Name()) || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("unexpected release file %q", entry.Name())
		}
	}

	manifest, err := os.Open(filepath.Join(dir, checksums))
	if err != nil {
		return err
	}
	defer func() { _ = manifest.Close() }()
	seen := make(map[string]bool, len(archives))
	lines := bufio.NewScanner(manifest)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) != 2 || !slices.Contains(archives, fields[1]) || seen[fields[1]] {
			return fmt.Errorf("unexpected checksum line %q", lines.Text())
		}
		digest, err := hashFile(filepath.Join(dir, fields[1]))
		if err != nil {
			return err
		}
		if digest != fields[0] {
			return fmt.Errorf("checksum mismatch for %q", fields[1])
		}
		seen[fields[1]] = true
	}
	if err := lines.Err(); err != nil {
		return err
	}
	if len(seen) != len(archives) {
		return errors.New("missing archive checksums")
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// Read only the named binary and write it to the already-open destination.
// Archive paths, links, and file permissions are never applied to the host.
func extractBinary(archive, name string, dst io.Writer) error {
	found := false
	write := func(src io.Reader, regular bool) error {
		if found || !regular {
			return fmt.Errorf("duplicate or non-regular %q in %s", name, archive)
		}
		found = true
		_, err := io.Copy(dst, src)
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		reader, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		for _, file := range reader.File {
			if file.Name != name {
				continue
			}
			src, err := file.Open()
			if err != nil {
				return err
			}
			err = errors.Join(write(src, file.Mode().IsRegular()), src.Close())
			if err != nil {
				return err
			}
		}
	} else {
		file, err := os.Open(archive)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		unpacked, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer func() { _ = unpacked.Close() }()
		reader := tar.NewReader(unpacked)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if header.Name == name {
				if err := write(reader, header.Typeflag == tar.TypeReg); err != nil {
					return err
				}
			}
		}
	}
	if !found {
		return fmt.Errorf("%q missing from %s", name, archive)
	}
	return nil
}

func verifyBinary(path, sha string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	want := map[string]string{
		"vcs": "git", "vcs.revision": sha, "vcs.modified": "false",
		"GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "CGO_ENABLED": "0",
	}
	for _, setting := range info.Settings {
		if expected, ok := want[setting.Key]; ok {
			if setting.Value != expected {
				return fmt.Errorf("binary %s = %q, expected %q", setting.Key, setting.Value, expected)
			}
			delete(want, setting.Key)
		}
	}
	if len(want) > 0 {
		return fmt.Errorf("binary missing build settings %v", want)
	}
	return nil
}

func smoke(ctx context.Context, archive, version, sha string) (err error) {
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(temp, "go-migrate-release-smoke-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	name := "gomigrate"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	dst, err := os.OpenFile(binary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	if err := errors.Join(extractBinary(archive, name, dst), dst.Close()); err != nil {
		return err
	}
	if err := verifyBinary(binary, sha); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if got, err := command(ctx, dir, binary, "--version"); err != nil {
		return err
	} else if got != "gomigrate version "+version {
		return fmt.Errorf("unexpected binary version %q, expected %q", got, version)
	}

	const migration = "-- +migrate Up\nCREATE TABLE release_smoke (id INTEGER PRIMARY KEY);\n-- +migrate Down\nDROP TABLE release_smoke;\n"
	if err := os.Mkdir(filepath.Join(dir, "migrations"), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "migrations", "0001_release_smoke.sql"), []byte(migration), 0o600); err != nil {
		return err
	}
	flags := []string{"--driver", "sqlite", "--dsn", "smoke.db", "--dir", "migrations", "-o", "json"}
	type cliResult struct {
		OK   bool
		Data struct {
			Current    string
			Pending    int
			Dirty      bool
			PlanID     string `json:"plan_id"`
			Migrations []json.RawMessage
			Applied    []json.RawMessage
		}
	}
	runCLI := func(wantExit int, args ...string) (cliResult, error) {
		var result cliResult
		out, err := command(ctx, dir, binary, append(slices.Clone(flags), args...)...)
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != wantExit {
				return result, err
			}
		} else if wantExit != 0 {
			return result, fmt.Errorf("%v exited with 0, expected %d", args, wantExit)
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			return result, fmt.Errorf("invalid JSON from %v: %w", args, err)
		}
		if !result.OK {
			return result, fmt.Errorf("SQLite smoke failed at %v: %s", args, out)
		}
		return result, nil
	}
	for _, step := range []struct {
		command string
		current string
		count   int
	}{
		{command: "status", count: 1},
		{command: "up", count: 1},
		{command: "status", current: "1"},
		{command: "validate"},
		{command: "up", count: 0},
		{command: "down", count: 1},
		{command: "status", count: 1},
		{command: "up", count: 1},
		{command: "status", current: "1"},
	} {
		switch step.command {
		case "status":
			exit := 0
			if step.count != 0 {
				exit = 2
			}
			result, err := runCLI(exit, "status", "--fail-on-pending")
			if err != nil {
				return err
			}
			if result.Data.Current != step.current || result.Data.Pending != step.count || result.Data.Dirty {
				return fmt.Errorf("unexpected SQLite status: current=%q pending=%d dirty=%t", result.Data.Current, result.Data.Pending, result.Data.Dirty)
			}
		case "up", "down":
			plan, err := runCLI(0, step.command, "--sql")
			if err != nil {
				return err
			}
			if plan.Data.PlanID == "" || len(plan.Data.Migrations) != step.count {
				return fmt.Errorf("unexpected SQLite %s plan: ID %q, %d migrations", step.command, plan.Data.PlanID, len(plan.Data.Migrations))
			}
			result, err := runCLI(0, step.command, "--expect-plan", plan.Data.PlanID)
			if err != nil {
				return err
			}
			if len(result.Data.Applied) != step.count {
				return fmt.Errorf("SQLite %s changed %d migrations, expected %d", step.command, len(result.Data.Applied), step.count)
			}
		default:
			if _, err := runCLI(0, step.command); err != nil {
				return err
			}
		}
	}
	_, err = fmt.Fprintf(os.Stdout, "SQLite smoke passed for %s at %s\n", filepath.Base(archive), sha)
	return err
}
