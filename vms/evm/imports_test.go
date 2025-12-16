// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	CORETH_IMPORT     = "github.com/ava-labs/avalanchego/graft/coreth"
	SUBNET_EVM_IMPORT = "github.com/ava-labs/avalanchego/graft/subnet-evm"
)

// TestImportViolations ensures proper import rules:
// - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)
// - graft/subnet-evm can only be imported within graft/subnet-evm itself and vms/evm/emulate
// - graft/evm can NOT import from graft/coreth or graft/subnet-evm
//
// The rationale for these rules are as follows:
//
// coreth can be imported in AvalancheGo, because it was already imported by Avalanche prior to
// grafting
//
// coreth can NOT be imported in the vms/evm package, because the goal is that vms/evm should
// only contain the 'clean' properly uplifted code, that meets AvalancheGo quality standards.
//
// subnet-evm can NOT be imported anywhere in AvalancheGo besides graft/subnet-evm itself,
// because it must not become a direct dependency of AvalancheGo or mix directly with coreth.
//
// graft/evm is the shared code that both coreth and subnet-evm will depend on, so it must not
// import from either of them to avoid a circular dependency.
//
// both coreth and subnet-evm can be imported in the vms/evm/emulate package, because it
// allows consumers to use both coreth and subnet-evm registration at the same time.
//
// TODO(jonathanoppenheimer): remove the graft functionality once the graft package is removed.
func TestImportViolations(t *testing.T) {
	const root = "../.."
	repoRoot, err := filepath.Abs(root)
	require.NoError(t, err)

	graftDir := filepath.Join(repoRoot, "graft")
	graftEVMDir := filepath.Join(graftDir, "evm")
	graftSubnetEVMDir := filepath.Join(graftDir, "subnet-evm")
	vmsEVMDir := filepath.Join(repoRoot, "vms", "evm")
	emulateDir := filepath.Join(vmsEVMDir, "emulate")

	var violations []string

	err = filepath.Walk(root, func(file string, _ fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.ToLower(filepath.Ext(file)) != ".go" {
			return nil
		}

		absFile, err := filepath.Abs(file)
		if err != nil {
			return err
		}

		node, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parser.ParseFile(..., %q, ...): %w", file, err)
		}

		for _, spec := range node.Imports {
			if spec.Path == nil {
				continue
			}
			importPath := strings.Trim(spec.Path.Value, `"`)

			inGraftEVM := strings.HasPrefix(absFile, graftEVMDir)
			inGraftSubnetEVM := strings.HasPrefix(absFile, graftSubnetEVMDir)
			inEmulate := strings.HasPrefix(absFile, emulateDir)
			inVMsEVM := strings.HasPrefix(absFile, vmsEVMDir)
			importsCoreth := isImportIn(importPath, CORETH_IMPORT)
			importsSubnetEVM := isImportIn(importPath, SUBNET_EVM_IMPORT)

			hasViolation := (importsCoreth && inVMsEVM && !inEmulate || // vm/evm can't import coreth
				importsSubnetEVM && !(inGraftSubnetEVM || inEmulate) || // vm/evm can't import subnet-evm
				(importsCoreth || importsSubnetEVM) && inGraftEVM) // graft/evm can't import coreth or subnet-evm
			if hasViolation {
				violations = append(violations, fmt.Sprintf("File %q imports %q", file, importPath))
			}
		}
		return nil
	})

	require.NoErrorf(t, err, "filepath.Walk(%q)", root)
	require.Empty(t, violations, "import violations found")
}

func isImportIn(importPath, targetedImport string) bool {
	if importPath == targetedImport {
		return true
	}

	// importPath must be at least len(targetedImport) + 1 to be a subpackage
	// (e.g., "github.com/foo/x" is len("github.com/foo") + 2, minimum subpackage length)
	if len(importPath) < len(targetedImport)+1 {
		return false
	}

	// Check if importPath is a subpackage by ensuring it has the targetedImport prefix
	// AND the next character is '/'. This is to prevent false positives where one
	// package name is a prefix of another like "github.com/foo" vs "github.com/foobar".
	return strings.HasPrefix(importPath, targetedImport) && importPath[len(targetedImport)] == '/'
}
