/**
 * @file generator.go
 * @description Checksum generator implementation with multiple algorithms
 * @author Admilson B. F. Cossa
 * @date 2024-12-30
 * @path pkg/core/database/migrations/internal/components/checksum/generator.go
 */

package checksum

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/adler32"
	"hash/crc32"
	"hash/crc64"
	"io"
	"os"
	"strings"

	"github.com/cespare/xxhash/v2"
	"golang.org/x/crypto/blake2b"
	"github.com/zeebo/blake3"

	"github.com/gogen-framework/gogen/pkg/core/database/migrations/internal/components/types"
)

// DefaultChecksumGenerator implements ChecksumGenerator interface
type DefaultChecksumGenerator struct {
	supportedAlgorithms map[ChecksumAlgorithm]bool
	hashFactories       map[ChecksumAlgorithm]func() hash.Hash
}

// NewDefaultChecksumGenerator creates a new default checksum generator
func NewDefaultChecksumGenerator() *DefaultChecksumGenerator {
	generator := &DefaultChecksumGenerator{
		supportedAlgorithms: make(map[ChecksumAlgorithm]bool),
		hashFactories:       make(map[ChecksumAlgorithm]func() hash.Hash),
	}

	// Register supported algorithms
	generator.registerAlgorithms()
	return generator
}

// registerAlgorithms registers all supported checksum algorithms
func (g *DefaultChecksumGenerator) registerAlgorithms() {
	// Standard crypto algorithms
	g.supportedAlgorithms[AlgorithmMD5] = true
	g.hashFactories[AlgorithmMD5] = func() hash.Hash { return md5.New() }

	g.supportedAlgorithms[AlgorithmSHA1] = true
	g.hashFactories[AlgorithmSHA1] = func() hash.Hash { return sha1.New() }

	g.supportedAlgorithms[AlgorithmSHA256] = true
	g.hashFactories[AlgorithmSHA256] = func() hash.Hash { return sha256.New() }

	g.supportedAlgorithms[AlgorithmSHA512] = true
	g.hashFactories[AlgorithmSHA512] = func() hash.Hash { return sha512.New() }

	// CRC algorithms
	g.supportedAlgorithms[AlgorithmCRC32] = true
	g.hashFactories[AlgorithmCRC32] = func() hash.Hash { return crc32.NewIEEE() }

	g.supportedAlgorithms[AlgorithmCRC64] = true
	g.hashFactories[AlgorithmCRC64] = func() hash.Hash { return crc64.New(crc64.MakeTable(crc64.ECMA)) }

	// Adler32
	g.supportedAlgorithms[AlgorithmAdler32] = true
	g.hashFactories[AlgorithmAdler32] = func() hash.Hash { return adler32.New() }

	// Blake2b
	g.supportedAlgorithms[AlgorithmBlake2b] = true
	g.hashFactories[AlgorithmBlake2b] = func() hash.Hash {
		h, _ := blake2b.New256(nil)
		return h
	}

	// Blake3
	g.supportedAlgorithms[AlgorithmBlake3] = true
	g.hashFactories[AlgorithmBlake3] = func() hash.Hash { return blake3.New() }

	// XXHash (special handling needed)
	g.supportedAlgorithms[AlgorithmXXHash] = true
}

// Generate generates a checksum for the given data using the specified algorithm
func (g *DefaultChecksumGenerator) Generate(data []byte, algorithm ChecksumAlgorithm) (string, error) {
	if err := g.ValidateAlgorithm(algorithm); err != nil {
		return "", err
	}

	// Special handling for XXHash
	if algorithm == AlgorithmXXHash {
		return g.generateXXHash(data), nil
	}

	// Use hash factory for other algorithms
	hashFunc := g.hashFactories[algorithm]
	if hashFunc == nil {
		return "", fmt.Errorf("hash factory not found for algorithm: %s", algorithm)
	}

	h := hashFunc()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GenerateFromReader generates a checksum from an io.Reader
func (g *DefaultChecksumGenerator) GenerateFromReader(reader io.Reader, algorithm ChecksumAlgorithm) (string, error) {
	if err := g.ValidateAlgorithm(algorithm); err != nil {
		return "", err
	}

	// Special handling for XXHash
	if algorithm == AlgorithmXXHash {
		return g.generateXXHashFromReader(reader)
	}

	// Use hash factory for other algorithms
	hashFunc := g.hashFactories[algorithm]
	if hashFunc == nil {
		return "", fmt.Errorf("hash factory not found for algorithm: %s", algorithm)
	}

	h := hashFunc()
	if _, err := io.Copy(h, reader); err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// GenerateFromFile generates a checksum from a file
func (g *DefaultChecksumGenerator) GenerateFromFile(filePath string, algorithm ChecksumAlgorithm) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	return g.GenerateFromReader(file, algorithm)
}

// GenerateFromMigration generates a checksum from a migration
func (g *DefaultChecksumGenerator) GenerateFromMigration(migration *types.Migration, algorithm ChecksumAlgorithm) (string, error) {
	if migration == nil {
		return "", fmt.Errorf("migration cannot be nil")
	}

	// Create a composite data representation of the migration
	migrationData := g.serializeMigration(migration)
	return g.Generate(migrationData, algorithm)
}

// GenerateComposite generates a composite checksum from multiple data sources
func (g *DefaultChecksumGenerator) GenerateComposite(data [][]byte, algorithm ChecksumAlgorithm) (string, error) {
	if err := g.ValidateAlgorithm(algorithm); err != nil {
		return "", err
	}

	if len(data) == 0 {
		return "", fmt.Errorf("no data provided for composite checksum")
	}

	// Special handling for XXHash
	if algorithm == AlgorithmXXHash {
		return g.generateCompositeXXHash(data), nil
	}

	// Use hash factory for other algorithms
	hashFunc := g.hashFactories[algorithm]
	if hashFunc == nil {
		return "", fmt.Errorf("hash factory not found for algorithm: %s", algorithm)
	}

	h := hashFunc()
	for i, chunk := range data {
		// Add separator between chunks
		if i > 0 {
			h.Write([]byte{0x00}) // null byte separator
		}
		h.Write(chunk)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// GetSupportedAlgorithms returns a list of supported algorithms
func (g *DefaultChecksumGenerator) GetSupportedAlgorithms() []ChecksumAlgorithm {
	algorithms := make([]ChecksumAlgorithm, 0, len(g.supportedAlgorithms))
	for algorithm := range g.supportedAlgorithms {
		algorithms = append(algorithms, algorithm)
	}
	return algorithms
}

// ValidateAlgorithm validates if the algorithm is supported
func (g *DefaultChecksumGenerator) ValidateAlgorithm(algorithm ChecksumAlgorithm) error {
	if !g.supportedAlgorithms[algorithm] {
		return fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
	return nil
}

// generateXXHash generates XXHash for data
func (g *DefaultChecksumGenerator) generateXXHash(data []byte) string {
	h := xxhash.New()
	h.Write(data)
	return fmt.Sprintf("%016x", h.Sum64())
}

// generateXXHashFromReader generates XXHash from reader
func (g *DefaultChecksumGenerator) generateXXHashFromReader(reader io.Reader) (string, error) {
	h := xxhash.New()
	if _, err := io.Copy(h, reader); err != nil {
		return "", fmt.Errorf("failed to read data: %w", err)
	}
	return fmt.Sprintf("%016x", h.Sum64()), nil
}

// generateCompositeXXHash generates composite XXHash
func (g *DefaultChecksumGenerator) generateCompositeXXHash(data [][]byte) string {
	h := xxhash.New()
	for i, chunk := range data {
		if i > 0 {
			h.Write([]byte{0x00}) // null byte separator
		}
		h.Write(chunk)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// serializeMigration serializes a migration to bytes for checksum calculation
func (g *DefaultChecksumGenerator) serializeMigration(migration *types.Migration) []byte {
	var parts []string

	// Include migration metadata
	parts = append(parts, fmt.Sprintf("id:%s", migration.ID))
	parts = append(parts, fmt.Sprintf("version:%s", migration.Version))
	parts = append(parts, fmt.Sprintf("name:%s", migration.Name))
	parts = append(parts, fmt.Sprintf("description:%s", migration.Description))

	// Include SQL content
	if migration.UpSQL != "" {
		parts = append(parts, fmt.Sprintf("up_sql:%s", migration.UpSQL))
	}
	if migration.DownSQL != "" {
		parts = append(parts, fmt.Sprintf("down_sql:%s", migration.DownSQL))
	}

	// Include dependencies
	if len(migration.Dependencies) > 0 {
		depStr := strings.Join(migration.Dependencies, ",")
		parts = append(parts, fmt.Sprintf("dependencies:%s", depStr))
	}

	// Include tags
	if len(migration.Tags) > 0 {
		tagStr := strings.Join(migration.Tags, ",")
		parts = append(parts, fmt.Sprintf("tags:%s", tagStr))
	}

	// Include metadata
	if len(migration.Metadata) > 0 {
		for key, value := range migration.Metadata {
			parts = append(parts, fmt.Sprintf("metadata:%s=%v", key, value))
		}
	}

	return []byte(strings.Join(parts, "|"))
}

// FastChecksumGenerator provides optimized checksum generation for performance-critical scenarios
type FastChecksumGenerator struct {
	*DefaultChecksumGenerator
	bufferSize int
}

// NewFastChecksumGenerator creates a new fast checksum generator
func NewFastChecksumGenerator(bufferSize int) *FastChecksumGenerator {
	if bufferSize <= 0 {
		bufferSize = 64 * 1024 // 64KB default buffer
	}

	return &FastChecksumGenerator{
		DefaultChecksumGenerator: NewDefaultChecksumGenerator(),
		bufferSize:               bufferSize,
	}
}

// GenerateFromReader generates checksum with buffered reading for better performance
func (g *FastChecksumGenerator) GenerateFromReader(reader io.Reader, algorithm ChecksumAlgorithm) (string, error) {
	if err := g.ValidateAlgorithm(algorithm); err != nil {
		return "", err
	}

	// Special handling for XXHash
	if algorithm == AlgorithmXXHash {
		return g.generateXXHashFromReaderBuffered(reader)
	}

	// Use hash factory for other algorithms
	hashFunc := g.hashFactories[algorithm]
	if hashFunc == nil {
		return "", fmt.Errorf("hash factory not found for algorithm: %s", algorithm)
	}

	h := hashFunc()
	buffer := make([]byte, g.bufferSize)

	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read data: %w", err)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// generateXXHashFromReaderBuffered generates XXHash with buffered reading
func (g *FastChecksumGenerator) generateXXHashFromReaderBuffered(reader io.Reader) (string, error) {
	h := xxhash.New()
	buffer := make([]byte, g.bufferSize)

	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read data: %w", err)
		}
	}

	return fmt.Sprintf("%016x", h.Sum64()), nil
}

// ParallelChecksumGenerator provides parallel checksum generation for multiple algorithms
type ParallelChecksumGenerator struct {
	*DefaultChecksumGenerator
	maxWorkers int
}

// NewParallelChecksumGenerator creates a new parallel checksum generator
func NewParallelChecksumGenerator(maxWorkers int) *ParallelChecksumGenerator {
	if maxWorkers <= 0 {
		maxWorkers = 4 // Default to 4 workers
	}

	return &ParallelChecksumGenerator{
		DefaultChecksumGenerator: NewDefaultChecksumGenerator(),
		maxWorkers:               maxWorkers,
	}
}

// GenerateMultiple generates checksums for multiple algorithms in parallel
func (g *ParallelChecksumGenerator) GenerateMultiple(data []byte, algorithms []ChecksumAlgorithm) (map[ChecksumAlgorithm]string, error) {
	if len(algorithms) == 0 {
		return nil, fmt.Errorf("no algorithms specified")
	}

	// Validate all algorithms first
	for _, algorithm := range algorithms {
		if err := g.ValidateAlgorithm(algorithm); err != nil {
			return nil, err
		}
	}

	results := make(map[ChecksumAlgorithm]string)
	errors := make(chan error, len(algorithms))
	resultChan := make(chan struct {
		algorithm ChecksumAlgorithm
		checksum  string
	}, len(algorithms))

	// Create worker pool
	semaphore := make(chan struct{}, g.maxWorkers)

	// Start workers
	for _, algorithm := range algorithms {
		go func(alg ChecksumAlgorithm) {
			semaphore <- struct{}{} // Acquire worker
			defer func() { <-semaphore }() // Release worker

			checksum, err := g.Generate(data, alg)
			if err != nil {
				errors <- fmt.Errorf("failed to generate %s checksum: %w", alg, err)
				return
			}

			resultChan <- struct {
				algorithm ChecksumAlgorithm
				checksum  string
			}{algorithm: alg, checksum: checksum}
		}(algorithm)
	}

	// Collect results
	for i := 0; i < len(algorithms); i++ {
		select {
		case result := <-resultChan:
			results[result.algorithm] = result.checksum
		case err := <-errors:
			return nil, err
		}
	}

	return results, nil
}

// GeneratorFactory creates different types of checksum generators
type GeneratorFactory struct{}

// NewGeneratorFactory creates a new generator factory
func NewGeneratorFactory() *GeneratorFactory {
	return &GeneratorFactory{}
}

// CreateGenerator creates a checksum generator based on the specified type
func (f *GeneratorFactory) CreateGenerator(generatorType string, config map[string]interface{}) (ChecksumGenerator, error) {
	switch generatorType {
	case "default":
		return NewDefaultChecksumGenerator(), nil
	case "fast":
		bufferSize := 64 * 1024 // Default buffer size
		if size, ok := config["buffer_size"].(int); ok {
			bufferSize = size
		}
		return NewFastChecksumGenerator(bufferSize), nil
	case "parallel":
		maxWorkers := 4 // Default workers
		if workers, ok := config["max_workers"].(int); ok {
			maxWorkers = workers
		}
		return NewParallelChecksumGenerator(maxWorkers), nil
	default:
		return nil, fmt.Errorf("unknown generator type: %s", generatorType)
	}
}

// GetSupportedGeneratorTypes returns supported generator types
func (f *GeneratorFactory) GetSupportedGeneratorTypes() []string {
	return []string{"default", "fast", "parallel"}
}

// GetDefaultConfig returns default configuration for a generator type
func (f *GeneratorFactory) GetDefaultConfig(generatorType string) map[string]interface{} {
	switch generatorType {
	case "fast":
		return map[string]interface{}{
			"buffer_size": 64 * 1024,
		}
	case "parallel":
		return map[string]interface{}{
			"max_workers": 4,
		}
	default:
		return map[string]interface{}{}
	}
}
