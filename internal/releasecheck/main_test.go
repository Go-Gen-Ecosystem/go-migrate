// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseSourceGate(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	git := func(args ...string) string {
		t.Helper()
		options := []string{"-c", "user.name=Release Test", "-c", "user.email=release@example.invalid", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}
		out, err := command(t.Context(), dir, "git", append(options, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	git("init", "--quiet")
	git("commit", "--quiet", "--allow-empty", "-m", "main")
	mainSHA := git("rev-parse", "HEAD")
	git("update-ref", "refs/remotes/origin/main", mainSHA)
	git("tag", "-a", "v1.2.3", "-m", "release")
	check := func(tag, sha, want string) {
		t.Helper()
		err := run(t.Context(), []string{"source", tag, sha})
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatalf("source %s %s = %v, want %q", tag, sha, err, want)
		}
	}
	check("v1.2.3", mainSHA, "")
	check("v1.2.3", strings.Repeat("0", 40), "HEAD resolves")
	check("v1.2.4", mainSHA, "refs/tags/v1.2.4")
	dirty := filepath.Join(dir, "untracked.txt")
	writeFixture(t, dirty, "uncommitted")
	check("v1.2.3", mainSHA, "release source must be clean")
	if err := os.Remove(dirty); err != nil {
		t.Fatal(err)
	}

	git("commit", "--quiet", "--allow-empty", "-m", "only in HEAD")
	offMainSHA := git("rev-parse", "HEAD")
	check("v1.2.3", offMainSHA, "refs/tags/v1.2.3^{commit} resolves")
	git("tag", "v1.2.4")
	check("v1.2.4", offMainSHA, "release commit must be on origin/main")
}

func TestReleasePayloadGate(t *testing.T) {
	const version = "1.2.3"
	const changed = "go-migrate_1.2.3_windows_arm64.zip"
	const first = "go-migrate_1.2.3_linux_amd64.tar.gz"
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string, []string)
		want   string
	}{
		{name: "six verified archives"},
		{name: "modified archive", change: func(t *testing.T, dir string, _ []string) {
			writeFixture(t, filepath.Join(dir, changed), "modified archive")
		}, want: "checksum mismatch"},
		{name: "missing archive", change: func(t *testing.T, dir string, _ []string) {
			if err := os.Remove(filepath.Join(dir, changed)); err != nil {
				t.Fatal(err)
			}
		}, want: "expected 8 release files"},
		{name: "extra payload file", change: func(t *testing.T, dir string, _ []string) {
			writeFixture(t, filepath.Join(dir, "unrelated.zip"), "unrelated")
		}, want: "expected 8 release files"},
		{name: "missing checksum", change: func(t *testing.T, dir string, lines []string) {
			writeFixture(t, filepath.Join(dir, checksums), strings.Join(lines[1:], "\n"))
		}, want: "missing archive checksums"},
		{name: "duplicate checksum", change: func(t *testing.T, dir string, lines []string) {
			lines[1] = lines[0]
			writeFixture(t, filepath.Join(dir, checksums), strings.Join(lines, "\n"))
		}, want: "unexpected checksum line"},
		{name: "path in checksum", change: func(t *testing.T, dir string, lines []string) {
			lines[0] = strings.Replace(lines[0], first, "../"+first, 1)
			writeFixture(t, filepath.Join(dir, checksums), strings.Join(lines, "\n"))
		}, want: "unexpected checksum line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			lines := make([]string, 0, 6)
			for _, name := range []string{
				first, "go-migrate_1.2.3_linux_arm64.tar.gz",
				"go-migrate_1.2.3_darwin_amd64.tar.gz", "go-migrate_1.2.3_darwin_arm64.tar.gz",
				"go-migrate_1.2.3_windows_amd64.zip", changed,
			} {
				writeFixture(t, filepath.Join(dir, name), name)
				lines = append(lines, fmt.Sprintf("%x  %s", sha256.Sum256([]byte(name)), name))
			}
			writeFixture(t, filepath.Join(dir, checksums), strings.Join(lines, "\n"))
			writeFixture(t, filepath.Join(dir, changelog), "## Changelog")
			if tc.change != nil {
				tc.change(t, dir, lines)
			}
			err := run(t.Context(), []string{"payload", "v" + version, dir})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("payload = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExtractReleaseBinary(t *testing.T) {
	t.Run("win zip ignores all paths except the binary", func(t *testing.T) {
		var content bytes.Buffer
		archive := zip.NewWriter(&content)
		for _, entry := range []string{"../outside.exe", "gomigrate.exe"} {
			writer, err := archive.Create(entry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprint(writer, entry); err != nil {
				t.Fatal(err)
			}
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "archive.zip")
		writeFixture(t, path, content.String())
		var dst bytes.Buffer
		if err := extractBinary(path, "gomigrate.exe", &dst); err != nil || dst.String() != "gomigrate.exe" {
			t.Fatalf("extract got %q, %v", dst.String(), err)
		}
	})

	t.Run("tar rejects a binary symlink", func(t *testing.T) {
		var content bytes.Buffer
		compressor := gzip.NewWriter(&content)
		archive := tar.NewWriter(compressor)
		if err := archive.WriteHeader(&tar.Header{Name: "gomigrate", Typeflag: tar.TypeSymlink, Linkname: "../outside"}); err != nil {
			t.Fatal(err)
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressor.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "archive.tar.gz")
		writeFixture(t, path, content.String())
		if err := extractBinary(path, "gomigrate", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("extract symlink = %v", err)
		}
	})
}

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
