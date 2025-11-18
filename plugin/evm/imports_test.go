// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

const repo = "github.com/ava-labs/coreth"

// getDependencies takes a fully qualified package name and returns a map of all
// its recursive package imports (including itself) in the same format.
func getDependencies(packageName string) (map[string]struct{}, error) {
	// Configure the load mode to include dependencies
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedName}
	pkgs, err := packages.Load(cfg, packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	if len(pkgs) == 0 || pkgs[0].Errors != nil {
		return nil, fmt.Errorf("failed to load package %s", packageName)
	}

	deps := make(map[string]struct{})
	var collectDeps func(pkg *packages.Package)
	collectDeps = func(pkg *packages.Package) {
		if _, ok := deps[pkg.PkgPath]; ok {
			return // Avoid re-processing the same dependency
		}
		deps[pkg.PkgPath] = struct{}{}
		for _, dep := range pkg.Imports {
			collectDeps(dep)
		}
	}

	// Start collecting dependencies
	for _, pkg := range pkgs {
		collectDeps(pkg)
	}
	return deps, nil
}

func TestMustNotImport(t *testing.T) {
	withRepo := func(pkg string) string {
		return fmt.Sprintf("%s/%s", repo, pkg)
	}
	mustNotImport := map[string][]string{
		// The following sub-packages of plugin/evm must not import core, core/vm
		// so clients (e.g., wallets, e2e tests) can import them without pulling in
		// the entire VM logic.
		// Importing these packages configures libevm globally and it is not
		// possible to do so for both coreth and subnet-evm, where the client may
		// wish to connect to multiple chains.

		"plugin/evm/atomic":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/client":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/config":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/customheader": {"core", "core/extstate", "core/vm", "params"},
		"ethclient":               {"plugin/evm/customtypes", "core/extstate", "params"},
		"warp":                    {"plugin/evm/customtypes", "core/extstate", "params"},
	}

	for packageName, forbiddenImports := range mustNotImport {
		imports, err := getDependencies(withRepo(packageName))
		require.NoError(t, err)

		for _, forbiddenImport := range forbiddenImports {
			fullForbiddenImport := withRepo(forbiddenImport)
			_, found := imports[fullForbiddenImport]
			require.False(t, found, "package %s must not import %s, check output of go list -f '{{ .Deps }}' \"%s\" ", packageName, fullForbiddenImport, withRepo(packageName))
		}
	}
}

// TestLibevmImportsAreAllowed ensures that all libevm imports in the codebase
// are explicitly allowed via the eth-allowed-packages.txt file.
func TestLibevmImportsAreAllowed(t *testing.T) {
	allowedPackages, err := loadAllowedPackages("../../scripts/eth-allowed-packages.txt")
	require.NoError(t, err, "Failed to load allowed packages")

	// Find all libevm imports in source files with proper filtering
	foundImports, err := findFilteredLibevmImportsWithFiles("../..")
	require.NoError(t, err, "Failed to find libevm imports")

	var disallowedImports set.Set[string]
	for importPath := range foundImports {
		if !allowedPackages.Contains(importPath) {
			disallowedImports.Add(importPath)
		}
	}

	if len(disallowedImports) == 0 {
		return
	}

	// After this point, there are disallowed imports, and the test will fail.
	// The remaining code is just necessary to pretty-print the error message,
	// to make it easier to find and fix the disallowed imports.
	sortedDisallowed := disallowedImports.List()
	slices.Sort(sortedDisallowed)

	var errorMsg strings.Builder
	errorMsg.WriteString("New libevm imports should be added to ./scripts/eth-allowed-packages.txt to prevent accidental imports:\n\n")
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

// findFilteredLibevmImportsWithFiles finds all libevm imports in the codebase,
// excluding underscore imports and "eth*" named imports.
// Returns a map of import paths to the set of files that contain them
func findFilteredLibevmImportsWithFiles(rootDir string) (map[string]set.Set[string], error) {
	imports := make(map[string]set.Set[string])
	libevmRegex := regexp.MustCompile(`^github\.com/ava-labs/libevm/`)

	err := filepath.Walk(rootDir, func(path string, _ os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".go") {
			return err
		}

		// Skip generated files, main_test.go, and tempextrastest directory
		filename := filepath.Base(path)
		if strings.HasPrefix(filename, "gen_") || strings.Contains(path, "core/main_test.go") || strings.Contains(path, "tempextrastest/") {
			return nil
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
			if !libevmRegex.MatchString(importPath) {
				continue
			}

			// Skip underscore and "eth*" named imports
			if imp.Name != nil && (imp.Name.Name == "_" || strings.HasPrefix(imp.Name.Name, "eth")) {
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
