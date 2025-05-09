// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// getDependencies takes a fully qualified package name and returns a map of all
// its recursive package imports (including itself) in the same format.
func getDependencies(packageName string) (map[string]struct{}, error) {
	// Configure the load mode to include dependencies
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedName}
	pkgs, err := packages.Load(cfg, packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %s", packageName)
	}

	if pkgs[0].Errors != nil {
		return nil, fmt.Errorf("failed to load package %s, %v", packageName, pkgs[0].Errors)
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
	withRepo := func(pkg string, repo string) string {
		return fmt.Sprintf("%s/%s", repo, pkg)
	}
	sourceRepo := "github.com/ava-labs/avalanchego"
	targetRepo := "github.com/ava-labs/coreth"
	mustNotImport := map[string][]string{
		// The following packages must not import core, core/vm
		// so clients (e.g., wallets, e2e tests) can import them without pulling in
		// the entire VM logic.
		// Importing these packages configures libevm globally and it is not
		// possible to do so for both coreth and subnet-evm, where the client may
		// wish to connect to multiple chains.
		"tests/...": {"plugin/evm/customtypes", "params"},
	}

	for packageName, forbiddenImports := range mustNotImport {
		imports, err := getDependencies(withRepo(packageName, sourceRepo))
		require.NoError(t, err)

		for _, forbiddenImport := range forbiddenImports {
			fullForbiddenImport := withRepo(forbiddenImport, targetRepo)
			_, found := imports[fullForbiddenImport]
			require.False(t, found, "package %s must not import %s, check output of go list -f '{{ .Deps }}' \"%s\" ", packageName, fullForbiddenImport, withRepo(packageName, sourceRepo))
		}
	}
}
