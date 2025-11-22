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

		// graft/coreth: blocked in vms/evm except vms/evm/emulate
		if isCoreth && isInVmsEvm && !isInEmulate {
			return false
		}

		// graft/subnet-evm: blocked everywhere except vms/evm/emulate
		if isSubnetEVM && !isInEmulate {
			return false
		}

		return true
	})
	require.NoError(t, err, "Failed to find graft imports")

	if len(foundImports) == 0 {
		return // no violations found
	}

	// After this point, there are illegal imports from the graft directory, and the test will fail.
	// The remaining code is just necessary to pretty-print the error message,
	// to make it easier to find and fix the disallowed imports.
	sortedImports := make([]string, 0, len(foundImports))
	for importPath := range foundImports {
		sortedImports = append(sortedImports, importPath)
	}
	slices.Sort(sortedImports)

	var errorMsg strings.Builder
	errorMsg.WriteString("Graft import rules violated!\n")
	errorMsg.WriteString("Rules:\n")
	errorMsg.WriteString("  - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)\n")
	errorMsg.WriteString("  - graft/subnet-evm cannot be imported anywhere EXCEPT vms/evm/emulate\n\n")
	errorMsg.WriteString("Violations:\n\n")

	for _, importPath := range sortedImports {
		files := foundImports[importPath]
		fileList := files.List()
		slices.Sort(fileList)

		errorMsg.WriteString(fmt.Sprintf("- %s\n", importPath))
		errorMsg.WriteString(fmt.Sprintf("   Used in %d file(s):\n", len(fileList)))
		for _, file := range fileList {
			errorMsg.WriteString(fmt.Sprintf("   • %s\n", file))
		}
		errorMsg.WriteString("\n")
	}
	require.Fail(t, errorMsg.String())
}

// TestLibevmImportsAreAllowed ensures that all libevm imports in the graft directory
// are explicitly allowed via the libevm-allowed-packages.txt file.
func TestLibevmImportsAreAllowed(t *testing.T) {
	allowedPackages, err := loadAllowedPackages("../scripts/libevm-allowed-packages.txt")
	require.NoError(t, err, "Failed to load allowed packages")

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

	var disallowedImports set.Set[string]
	for importPath := range foundImports {
		if !allowedPackages.Contains(importPath) {
			disallowedImports.Add(importPath)
		}
	}

	if len(disallowedImports) == 0 {
		return // no violations found
	}

	// After this point, there are disallowed imports, and the test will fail.
	// The remaining code is just necessary to pretty-print the error message,
	// to make it easier to find and fix the disallowed imports.
	sortedDisallowed := disallowedImports.List()
	slices.Sort(sortedDisallowed)

	var errorMsg strings.Builder
	errorMsg.WriteString("Files inside the graft directory must not import forbidden libevm packages!\nIf a package is safe to import, add it to ./scripts/libevm-allowed-packages.txt.\n\n")
	for _, importPath := range sortedDisallowed {
		files := foundImports[importPath]
		fileList := files.List()
		slices.Sort(fileList)

		errorMsg.WriteString(fmt.Sprintf("- %s\n", importPath))
		errorMsg.WriteString(fmt.Sprintf("   Used in %d file(s):\n", len(fileList)))
		for _, file := range fileList {
			errorMsg.WriteString(fmt.Sprintf("   • %s\n", file))
		}
		errorMsg.WriteString("\n")
	}
	require.Fail(t, errorMsg.String())
}

// loadAllowedPackages reads the allowed packages from the specified file
func loadAllowedPackages(filename string) (set.Set[string], error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open allowed packages file: %w", err)
	}
	defer file.Close()

	allowed := set.Set[string]{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.Trim(line, `"`)
		allowed.Add(line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read allowed packages file: %w", err)
	}

	return allowed, nil
}

// importFilter is a function that can filter imports based on the file path,
// import path, and AST import spec (which contains the import name).
// Return true to skip this import.
type importFilter func(filePath string, importPath string, importSpec *ast.ImportSpec) bool

// findImportsMatchingPattern is a generalized function that finds all imports
// matching a given regex pattern in the specified directory.
// The filterFunc can be used to skip certain files or imports (return true to skip).
// Returns a map of import paths to the set of files that contain them.
func findImportsMatchingPattern(
	rootDir string,
	importRegex *regexp.Regexp,
	filterFunc importFilter,
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
