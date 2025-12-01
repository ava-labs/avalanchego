// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImportViolations ensures proper import rules:
// - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)
// - graft/subnet-evm cannot be imported anywhere EXCEPT vms/evm/emulate (but vms/evm/emulate is an exception)
// - github.com/ava-labs/libevm/libevm/pseudo cannot be imported anywhere
func TestImportViolations(t *testing.T) {
	const root = ".."
	err := filepath.Walk(root, func(file string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.ToLower(filepath.Ext(file)) != "go" {
			return nil
		}

		node, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parser.ParseFile(..., %q, ...): %v", file, err)
		}

		for _, spec := range node.Imports {
			if spec.Path == nil {
				continue
			}
			imp := strings.Trim(spec.Path.Value, `"`)

			inGraft := strings.Contains(file, "/graft")
			inEmulate := strings.Contains(file, "/vms/evm/emulate")
			inVMsEVM := strings.Contains(file, "/vms/evm")
			importsPseudo := isPackageIn(imp, "github.com/ava-labs/libevm/libevm/pseudo")
			importsCoreth := isPackageIn(imp, "github.com/ava-labs/avalanchego/graft/coreth")
			importsSubnetEVM := isPackageIn(imp, "github.com/ava-labs/avalanchego/graft/subnet-evm")

			// coreth can be imported in AvalancheGo, because it was already imported by Avalanche prior to
			// grafting
			//
			// coreth can NOT be imported in the vms/evm package, because the goal is that vms/evm should
			// only contain the 'clean' properly uplifted code, that meets AvalancheGo quality standards.
			//
			// subnet-evm can NOT be imported anywhere in AvalancheGo besides /graft, because it must
			// not become a direct dependency of AvalancheGo.
			//
			// both coreth and subnet-evm can be imported in the vms/evm/emulate package, because it is a
			// temporary package that allows consumers to import both coreth and subnet-evm at the same time.
			//
			// github.com/ava-labs/libevm/libevm/pseudo cannot be imported anywhere, because it requires so
			// much care to avoid catastrophic misuse and there are so few reasons to ever use it anyway
			//
			// TODO(jonathanoppenheimer): remove the emulate functionality once the emulate package is removed.
			// TODO(jonathanoppenheimer): remove the graft functionality once the graft package will be removed.
			violations := []bool{
				importsPseudo,
				!inGraft && importsCoreth && inVMsEVM && !inEmulate,
				!inGraft && importsSubnetEVM && !inEmulate,
			}
			if slices.Contains(violations, true) {
				t.Errorf("File %q imports %q", file, imp)
			}
		}
		return nil
	})

	require.NoErrorf(t, err, "filepath.Walk(%q)", root)
}

func isPackageIn(importPath, root string) bool {
	if importPath == root {
		return true
	}
	if len(importPath) < len(root)+1 {
		return false
	}
	return strings.HasPrefix(importPath, root) && importPath[len(root)] == '/'
}
