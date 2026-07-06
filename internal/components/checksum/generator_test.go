/**
 * @file generator_test.go
 * @description Known-answer tests (KATs) and behavior tests for the checksum
 *              generators across every supported algorithm, plus reader/file/
 *              migration/composite variants and the fast/parallel generators.
 * @author Admilson B. F. Cossa
 * @date 2026-07-06
 * @path internal/components/checksum/generator_test.go
 */

package checksum

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
)

// abcKAT holds the reference hex digests for the ASCII string "abc" produced by
// the standard library, used to assert the generator matches canonical output.
var abcData = []byte("abc")

func TestGenerate_KnownAnswers(t *testing.T) {
	g := NewDefaultChecksumGenerator()

	md5Sum := md5.Sum(abcData)
	sha1Sum := sha1.Sum(abcData)
	sha256Sum := sha256.Sum256(abcData)
	sha512Sum := sha512.Sum512(abcData)

	cases := []struct {
		algo ChecksumAlgorithm
		want string
	}{
		{AlgorithmMD5, hex.EncodeToString(md5Sum[:])},
		{AlgorithmSHA1, hex.EncodeToString(sha1Sum[:])},
		{AlgorithmSHA256, hex.EncodeToString(sha256Sum[:])},
		{AlgorithmSHA512, hex.EncodeToString(sha512Sum[:])},
		// Well-known constant digests.
		{AlgorithmSHA256, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, c := range cases {
		got, err := g.Generate(abcData, c.algo)
		if err != nil {
			t.Fatalf("Generate(%s): %v", c.algo, err)
		}
		if got != c.want {
			t.Errorf("Generate(%s) = %s, want %s", c.algo, got, c.want)
		}
	}
}

func TestGenerate_AllSupportedAlgorithmsProduceOutput(t *testing.T) {
	g := NewDefaultChecksumGenerator()
	for _, algo := range g.GetSupportedAlgorithms() {
		got, err := g.Generate(abcData, algo)
		if err != nil {
			t.Errorf("Generate(%s): %v", algo, err)
			continue
		}
		if got == "" {
			t.Errorf("Generate(%s) produced empty output", algo)
		}
	}
}

func TestGenerate_XXHashDeterministic(t *testing.T) {
	g := NewDefaultChecksumGenerator()
	a, err := g.Generate(abcData, AlgorithmXXHash)
	if err != nil {
		t.Fatalf("xxhash: %v", err)
	}
	b, _ := g.Generate(abcData, AlgorithmXXHash)
	if a != b {
		t.Errorf("xxhash not deterministic: %s != %s", a, b)
	}
	if len(a) != 16 {
		t.Errorf("xxhash output length = %d, want 16 hex chars", len(a))
	}
}

func TestGenerate_UnsupportedAlgorithm(t *testing.T) {
	g := NewDefaultChecksumGenerator()
	if _, err := g.Generate(abcData, ChecksumAlgorithm("nope")); err == nil {
		t.Errorf("expected error for unsupported algorithm")
	}
	if err := g.ValidateAlgorithm(ChecksumAlgorithm("nope")); err == nil {
		t.Errorf("ValidateAlgorithm should reject unknown algorithm")
	}
	if err := g.ValidateAlgorithm(AlgorithmSHA256); err != nil {
		t.Errorf("ValidateAlgorithm(sha256) unexpected error: %v", err)
	}
}

func TestGenerateFromReader_MatchesGenerate(t *testing.T) {
	g := NewDefaultChecksumGenerator()
	for _, algo := range []ChecksumAlgorithm{AlgorithmSHA256, AlgorithmXXHash, AlgorithmCRC32} {
		want, _ := g.Generate(abcData, algo)
		got, err := g.GenerateFromReader(bytes.NewReader(abcData), algo)
		if err != nil {
			t.Fatalf("GenerateFromReader(%s): %v", algo, err)
		}
		if got != want {
			t.Errorf("reader vs direct mismatch for %s: %s != %s", algo, got, want)
		}
	}
}

func TestGenerateFromFile(t *testing.T) {
	g := NewDefaultChecksumGenerator()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.sql")
	if err := os.WriteFile(path, abcData, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	want, _ := g.Generate(abcData, AlgorithmSHA256)
	got, err := g.GenerateFromFile(path, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GenerateFromFile: %v", err)
	}
	if got != want {
		t.Errorf("file checksum mismatch: %s != %s", got, want)
	}

	// Missing file should error.
	if _, err := g.GenerateFromFile(filepath.Join(dir, "missing"), AlgorithmSHA256); err == nil {
		t.Errorf("expected error for missing file")
	}
}

func TestGenerateFromMigration(t *testing.T) {
	g := NewDefaultChecksumGenerator()

	m := &types.Migration{
		ID:          "V1",
		Version:     "1",
		FileName:    "V1__init.sql",
		Description: "init",
		Script:      "CREATE TABLE t(id INT);",
		Dependencies: []string{"V0"},
		Tags:        []string{"schema"},
	}
	c1, err := g.GenerateFromMigration(m, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GenerateFromMigration: %v", err)
	}
	if c1 == "" {
		t.Fatalf("empty migration checksum")
	}

	// Same migration -> same checksum (stability).
	c2, _ := g.GenerateFromMigration(m, AlgorithmSHA256)
	if c1 != c2 {
		t.Errorf("migration checksum unstable: %s != %s", c1, c2)
	}

	// Changing the script must change the checksum.
	m2 := *m
	m2.Script = "CREATE TABLE t(id BIGINT);"
	c3, _ := g.GenerateFromMigration(&m2, AlgorithmSHA256)
	if c3 == c1 {
		t.Errorf("changing script did not change checksum")
	}

	// Nil migration must error.
	if _, err := g.GenerateFromMigration(nil, AlgorithmSHA256); err == nil {
		t.Errorf("expected error for nil migration")
	}
}

func TestGenerateComposite(t *testing.T) {
	g := NewDefaultChecksumGenerator()

	c, err := g.GenerateComposite([][]byte{[]byte("a"), []byte("b")}, AlgorithmSHA256)
	if err != nil {
		t.Fatalf("GenerateComposite: %v", err)
	}
	if c == "" {
		t.Fatalf("empty composite checksum")
	}

	// Order matters (separator + ordering) -> different chunk order differs.
	c2, _ := g.GenerateComposite([][]byte{[]byte("b"), []byte("a")}, AlgorithmSHA256)
	if c == c2 {
		t.Errorf("composite checksum should depend on chunk order")
	}

	// XXHash composite path.
	if _, err := g.GenerateComposite([][]byte{[]byte("a")}, AlgorithmXXHash); err != nil {
		t.Errorf("composite xxhash: %v", err)
	}

	// Empty input must error.
	if _, err := g.GenerateComposite(nil, AlgorithmSHA256); err == nil {
		t.Errorf("expected error for empty composite data")
	}

	// Unsupported algorithm must error.
	if _, err := g.GenerateComposite([][]byte{[]byte("a")}, ChecksumAlgorithm("x")); err == nil {
		t.Errorf("expected error for unsupported composite algorithm")
	}
}

func TestFastChecksumGenerator(t *testing.T) {
	def := NewDefaultChecksumGenerator()
	fast := NewFastChecksumGenerator(0) // 0 -> default buffer

	want, _ := def.Generate(abcData, AlgorithmSHA256)
	got, err := fast.GenerateFromReader(bytes.NewReader(abcData), AlgorithmSHA256)
	if err != nil {
		t.Fatalf("fast GenerateFromReader: %v", err)
	}
	if got != want {
		t.Errorf("fast reader mismatch: %s != %s", got, want)
	}

	// XXHash buffered path.
	wantX, _ := def.Generate(abcData, AlgorithmXXHash)
	gotX, err := fast.GenerateFromReader(bytes.NewReader(abcData), AlgorithmXXHash)
	if err != nil {
		t.Fatalf("fast xxhash: %v", err)
	}
	if gotX != wantX {
		t.Errorf("fast xxhash mismatch: %s != %s", gotX, wantX)
	}

	// Unsupported algorithm.
	if _, err := fast.GenerateFromReader(bytes.NewReader(abcData), ChecksumAlgorithm("x")); err == nil {
		t.Errorf("expected error for unsupported algorithm in fast generator")
	}
}

func TestParallelChecksumGenerator_GenerateMultiple(t *testing.T) {
	def := NewDefaultChecksumGenerator()
	par := NewParallelChecksumGenerator(2)

	algos := []ChecksumAlgorithm{AlgorithmMD5, AlgorithmSHA256, AlgorithmCRC32, AlgorithmXXHash}
	results, err := par.GenerateMultiple(abcData, algos)
	if err != nil {
		t.Fatalf("GenerateMultiple: %v", err)
	}
	if len(results) != len(algos) {
		t.Fatalf("expected %d results, got %d", len(algos), len(results))
	}
	for _, algo := range algos {
		want, _ := def.Generate(abcData, algo)
		if results[algo] != want {
			t.Errorf("parallel %s = %s, want %s", algo, results[algo], want)
		}
	}

	// Empty algorithm list errors.
	if _, err := par.GenerateMultiple(abcData, nil); err == nil {
		t.Errorf("expected error for empty algorithm list")
	}
	// Unsupported algorithm errors before any work.
	if _, err := par.GenerateMultiple(abcData, []ChecksumAlgorithm{ChecksumAlgorithm("x")}); err == nil {
		t.Errorf("expected error for unsupported algorithm")
	}
}

func TestGeneratorFactory(t *testing.T) {
	f := NewGeneratorFactory()

	types := f.GetSupportedGeneratorTypes()
	if len(types) != 3 {
		t.Errorf("expected 3 generator types, got %d", len(types))
	}

	for _, gt := range types {
		gen, err := f.CreateGenerator(gt, nil)
		if err != nil {
			t.Errorf("CreateGenerator(%s): %v", gt, err)
		}
		if gen == nil {
			t.Errorf("CreateGenerator(%s) returned nil", gt)
		}
		// Default config for a type must be usable.
		cfg := f.GetDefaultConfig(gt)
		if cfg == nil {
			t.Errorf("GetDefaultConfig(%s) returned nil", gt)
		}
	}

	// Unknown type errors.
	if _, err := f.CreateGenerator(GeneratorType("bogus"), nil); err == nil {
		t.Errorf("expected error for unknown generator type")
	}
}
