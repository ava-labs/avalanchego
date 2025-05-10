// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package packages

import (
	"fmt"

	"golang.org/x/tools/go/packages"
)

// GetDependencies takes a fully qualified package name and returns a map of all
// its recursive package imports (including itself) in the same format.
func GetDependencies(packageName string) (map[string]struct{}, error) {
	// Configure the load mode to include dependencies
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedName}
	pkgs, err := packages.Load(cfg, packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %s", packageName)
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
		if pkg.Errors != nil {
			return nil, fmt.Errorf("failed to load package %s, %v", packageName, pkg.Errors)
		}
		collectDeps(pkg)
	}
	return deps, nil
}
