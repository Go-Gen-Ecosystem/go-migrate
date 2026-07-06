/**
 * @file remove_debug_prints.go
 * @description Script to remove all debug print statements from production code
 * @author Claude Code
 * @date 2025-01-01
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DebugPattern struct {
	Pattern     *regexp.Regexp
	Description string
	Replacement string
}

var debugPatterns = []DebugPattern{
	{
		Pattern:     regexp.MustCompile(`^\s*fmt\.Printf\("DEBUG:.*\n`),
		Description: "fmt.Printf DEBUG single line",
		Replacement: "",
	},
	{
		Pattern:     regexp.MustCompile(`^\s*fmt\.Printf\("\[DEBUG\].*\n`),
		Description: "fmt.Printf [DEBUG] single line",
		Replacement: "",
	},
	{
		Pattern:     regexp.MustCompile(`^\s*fmt\.Println\("DEBUG:.*\n`),
		Description: "fmt.Println DEBUG single line",
		Replacement: "",
	},
}

func main() {
	baseDir := "."
	if len(os.Args) > 1 {
		baseDir = os.Args[1]
	}

	fmt.Printf("Removing debug print statements from: %s\n", baseDir)

	stats := &Stats{
		FilesScanned: 0,
		FilesChanged: 0,
		LinesRemoved: 0,
	}

	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories, non-Go files, and vendor/node_modules
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		if strings.Contains(path, "vendor") || strings.Contains(path, "node_modules") {
			return nil
		}

		// Process the file
		return processFile(path, stats)
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Files scanned: %d\n", stats.FilesScanned)
	fmt.Printf("Files changed: %d\n", stats.FilesChanged)
	fmt.Printf("Lines removed: %d\n", stats.LinesRemoved)

	if stats.FilesChanged > 0 {
		fmt.Printf("\n✅ Debug prints removed successfully!\n")
		fmt.Printf("Run 'go fmt ./...' to format the files\n")
	} else {
		fmt.Printf("\n✨ No debug prints found - code is clean!\n")
	}
}

type Stats struct {
	FilesScanned int
	FilesChanged int
	LinesRemoved int
}

func processFile(path string, stats *Stats) error {
	stats.FilesScanned++

	// Read file
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()

	var lines []string
	var removedLines int
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		// Check if line matches any debug pattern
		shouldRemove := false
		for _, pattern := range debugPatterns {
			if pattern.Pattern.MatchString(line) {
				shouldRemove = true
				removedLines++
				break
			}
		}

		if !shouldRemove {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to scan %s: %w", path, err)
	}

	// If no lines were removed, skip writing
	if removedLines == 0 {
		return nil
	}

	// Write back to file
	output := strings.Join(lines, "\n")
	if !strings.HasSuffix(output, "\n") && len(output) > 0 {
		output += "\n"
	}

	err = os.WriteFile(path, []byte(output), 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	stats.FilesChanged++
	stats.LinesRemoved += removedLines

	fmt.Printf("✓ %s: removed %d debug lines\n", path, removedLines)

	return nil
}
