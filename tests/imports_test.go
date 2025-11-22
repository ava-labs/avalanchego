// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

// TestEnforceLicensingImportBoundaries ensures that upstream files (listed in
// graft/scripts/forbidden-geth-files.txt) cannot be imported outside the graft directory,
// except for explicitly allowed exceptions (lines without !).
func TestEnforceLicensingImportBoundaries(t *testing.T) {
	forbiddenPatterns, allowedExceptions, err := loadPatternFile("../graft/scripts/forbidden-geth-files.txt")
	require.NoError(t, err, "Failed to load forbidden geth files patterns")

	// Find all graft/coreth and graft/subnet-evm imports outside the graft directory
	graftRegex := regexp.MustCompile(`^github\.com/ava-labs/avalanchego/graft/(coreth|subnet-evm)/`)
	foundImports, err := findImportsMatchingPattern("..", graftRegex, func(path string, _ string, _ *ast.ImportSpec) bool {
		// Skip files within the graft directory - they can import anything from graft
		return strings.Contains(path, "/graft/")
	})
	require.NoError(t, err, "Failed to find graft imports")

	violations := make(map[string]set.Set[string])
	for importPath, files := range foundImports {
		// Convert import path to pattern-compatible format
		// e.g., "github.com/ava-labs/avalanchego/graft/coreth/core/state" -> "graft/coreth/core/state"
		pathForMatching := strings.TrimPrefix(importPath, "github.com/ava-labs/avalanchego/")

		// Check if this path matches any allowed exception
		isAllowed := false
		for _, allowed := range allowedExceptions {
			if matched, _ := filepath.Match(allowed, pathForMatching); matched {
				isAllowed = true
				break
			}
		}

		if isAllowed {
			continue
		}

		// Check if this path matches any forbidden pattern
		isForbidden := false
		for _, forbidden := range forbiddenPatterns {
			if matched, _ := filepath.Match(forbidden, pathForMatching); matched {
				isForbidden = true
				break
			}
		}

		if isForbidden {
			violations[importPath] = files
		}
	}

	if len(violations) == 0 {
		return // no violations found
	}

	header := "Upstream file import rules violated!\n" +
		"Upstream files (from graft/coreth or graft/subnet-evm) cannot be imported outside the graft directory,\n" +
		"except for explicitly allowed exceptions in graft/scripts/forbidden-geth-files.txt (lines without !).\n\n"
	require.Fail(t, formatImportViolations(violations, header))
}

// TestDoNotImportFromGraft ensures proper import rules for graft packages:
// - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)
// - graft/subnet-evm cannot be imported anywhere EXCEPT vms/evm/emulate
func TestEnforceGraftImportBoundaries(t *testing.T) {
	graftRegex := regexp.MustCompile(`^github\.com/ava-labs/avalanchego/graft(/|$)`)

	// Find all graft imports in the entire repository
	foundImports, err := findImportsMatchingPattern("..", graftRegex, func(path string, importPath string, _ *ast.ImportSpec) bool {
		// Skip generated files and test-specific files
		filename := filepath.Base(path)
		if strings.HasPrefix(filename, "gen_") ||
			strings.Contains(path, "graft/*/core/main_test.go") ||
			strings.Contains(path, "graft/*/tempextrastest/") {
			return true
		}

		// Skip files in the graft directory itself - they can import from graft
		if strings.Contains(path, "/graft/") {
			return true
		}

		isInEmulate := strings.Contains(path, "/vms/evm/emulate/")
		isInVmsEvm := strings.Contains(path, "/vms/evm/")
		isCoreth := strings.Contains(importPath, "/graft/coreth")
		isSubnetEVM := strings.Contains(importPath, "/graft/subnet-evm")

		// graft/coreth: illegal in vms/evm except vms/evm/emulate
		if isCoreth && isInVmsEvm && !isInEmulate {
			return false
		}

		// graft/subnet-evm: illegal everywhere except vms/evm/emulate
		if isSubnetEVM && !isInEmulate {
			return false
		}

		return true
	})
	require.NoError(t, err, "Failed to find graft imports")

	if len(foundImports) == 0 {
		return // no violations found
	}

	header := "Graft import rules violated!\n" +
		"Rules:\n" +
		"  - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)\n" +
		"  - graft/subnet-evm cannot be imported anywhere EXCEPT vms/evm/emulate\n\n"
	require.Fail(t, formatImportViolations(foundImports, header))
}

// TestLibevmImportsAreAllowed ensures that all libevm imports in the graft directory
// are explicitly allowed via the libevm-allowed-packages.txt file.
func TestLibevmImportsAreAllowed(t *testing.T) {
	_, allowedPackages, err := loadPatternFile("../graft/scripts/libevm-allowed-packages.txt")
	require.NoError(t, err, "Failed to load allowed packages")

	// Convert allowed packages to a set for faster lookup
	allowedSet := set.Set[string]{}
	for _, pkg := range allowedPackages {
		allowedSet.Add(pkg)
	}

	// Find all libevm imports in source files, excluding underscore and "eth*" named imports
	libevmRegex := regexp.MustCompile(`^github\.com/ava-labs/libevm/`)
	foundImports, err := findImportsMatchingPattern("../graft", libevmRegex, func(path string, _ string, imp *ast.ImportSpec) bool {
		// Skip generated files and test-specific files
		filename := filepath.Base(path)
		if strings.HasPrefix(filename, "gen_") ||
			strings.Contains(path, "graft/*/core/main_test.go") ||
			strings.Contains(path, "graft/*/tempextrastest/") {
			return true
		}

		// Skip underscore and "eth*" named imports
		return imp.Name != nil && (imp.Name.Name == "_" || strings.HasPrefix(imp.Name.Name, "eth"))
	})
	require.NoError(t, err, "Failed to find libevm imports")

	violations := make(map[string]set.Set[string])
	for importPath, files := range foundImports {
		if !allowedSet.Contains(importPath) {
			violations[importPath] = files
		}
	}

	if len(violations) == 0 {
		return // no violations found
	}

	header := "Files inside the graft directory must not import forbidden libevm packages!\n" +
		"If a package is safe to import, add it to graft/scripts/libevm-allowed-packages.txt.\n\n"
	require.Fail(t, formatImportViolations(violations, header))
}

// formatImportViolations formats a map of import violations into an error message
func formatImportViolations(violations map[string]set.Set[string], header string) string {
	sortedViolations := make([]string, 0, len(violations))
	for importPath := range violations {
		sortedViolations = append(sortedViolations, importPath)
	}
	slices.Sort(sortedViolations)

	var errorMsg strings.Builder
	errorMsg.WriteString(header)
	errorMsg.WriteString("Violations:\n\n")

	for _, importPath := range sortedViolations {
		files := violations[importPath]
		fileList := files.List()
		slices.Sort(fileList)

		errorMsg.WriteString(fmt.Sprintf("- %s\n", importPath))
		errorMsg.WriteString(fmt.Sprintf("   Used in %d file(s):\n", len(fileList)))
		for _, file := range fileList {
			errorMsg.WriteString(fmt.Sprintf("   • %s\n", file))
		}
		errorMsg.WriteString("\n")
	}

	return errorMsg.String()
}

// loadPatternFile reads patterns from a file and separates them into forbidden and allowed.
// Lines with a ! prefix are forbidden patterns.
// Lines without a ! prefix are allowed patterns (exceptions).
// Returns: (forbidden patterns, allowed patterns, error)
func loadPatternFile(filename string) ([]string, []string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open pattern file: %w", err)
	}
	defer file.Close()

	var forbidden []string
	var allowed []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.Trim(line, `"`)
		if strings.HasPrefix(line, "!") {
			forbidden = append(forbidden, strings.TrimPrefix(line, "!"))
		} else {
			allowed = append(allowed, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to read pattern file: %w", err)
	}

	return forbidden, allowed, nil
}

// findImportsMatchingPattern is a generalized function that finds all imports
// matching a given regex pattern in the specified directory.
// The filterFunc can be used to skip certain files or imports (return true to skip).
// Returns a map of import paths to the set of files that contain them.
func findImportsMatchingPattern(
	rootDir string,
	importRegex *regexp.Regexp,
	filterFunc func(filePath string, importPath string, importSpec *ast.ImportSpec) bool,
) (map[string]set.Set[string], error) {
	imports := make(map[string]set.Set[string])

	err := filepath.Walk(rootDir, func(path string, _ os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".go") {
			return err
		}

		node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}

		for _, imp := range node.Imports {
			if imp.Path == nil {
				continue
			}

			importPath := strings.Trim(imp.Path.Value, `"`)
			if !importRegex.MatchString(importPath) {
				continue
			}

			if filterFunc != nil && filterFunc(path, importPath, imp) {
				continue
			}

			if _, exists := imports[importPath]; !exists {
				imports[importPath] = set.Set[string]{}
			}
			fileSet := imports[importPath]
			fileSet.Add(path)
			imports[importPath] = fileSet
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return imports, nil
}
