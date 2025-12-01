// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
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

// TestDoNotImportLibevmPseudo ensures that no code in the repository imports
// the libevm/pseudo package, which is for internal libevm use only.
func TestDoNotImportLibevmPseudo(t *testing.T) {
	// Find imports from the forbidden libevm/pseudo package
	pseudoRegex := regexp.MustCompile(`^github\.com/ava-labs/libevm/libevm/pseudo`)
	foundImports, err := findImportsMatchingPattern("../..", pseudoRegex, nil)
	require.NoError(t, err, "Failed to scan for libevm/pseudo imports")

	if len(foundImports) == 0 {
		return // no violations found
	}

	header := "Files must not import libevm/pseudo!\n" +
		"The pseudo package is for internal libevm use only.\n\n"
	require.Fail(t, formatImportViolations(foundImports, header))
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
