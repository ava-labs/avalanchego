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

const root = ".."

// TestImportViolations ensures proper import rules:
// - graft/coreth can be imported anywhere EXCEPT vms/evm (but vms/evm/emulate is an exception)
// - graft/subnet-evm cannot be imported anywhere EXCEPT vms/evm/emulate (but vms/evm/emulate is an exception)
// - github.com/ava-labs/libevm/libevm/pseudo cannot be imported anywhere
//
// The rationale for these rules are as follows:
//
// coreth can be imported in AvalancheGo, because it was already imported by Avalanche prior to
// grafting
//
// coreth can NOT be imported in the vms/evm package, because the goal is that vms/evm should
// only contain the 'clean' properly uplifted code, that meets AvalancheGo quality standards.
//
// subnet-evm can NOT be imported anywhere in AvalancheGo besides /graft, because it must
// not become a direct dependency of AvalancheGo.
//
// both coreth and subnet-evm can be imported in the vms/evm/emulate package, because it
// allows consumers to use both coreth and subnet-evm registration at the same time.
//
// github.com/ava-labs/libevm/libevm/pseudo cannot be imported anywhere, without review from any of
// @StephenButtolph, @ARR4N, or @joshua-kim. There is almost certainly a better option, and it exists
// only because libevm can't pollute the code base with generic type parameters.
//
// TODO(jonathanoppenheimer): remove the graft functionality once the graft package will be removed.
func TestImportViolations(t *testing.T) {
	var violations []string

	err := filepath.Walk(root, func(file string, _ fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.ToLower(filepath.Ext(file)) != ".go" {
			return nil
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

			inGraft := strings.Contains(file, "/graft")
			inEmulate := strings.Contains(file, "/vms/evm/emulate")
			inVMsEVM := strings.Contains(file, "/vms/evm")
			importsPseudo := isImportIn(importPath, "github.com/ava-labs/libevm/libevm/pseudo")
			importsCoreth := isImportIn(importPath, "github.com/ava-labs/avalanchego/graft/coreth")
			importsSubnetEVM := isImportIn(importPath, "github.com/ava-labs/avalanchego/graft/subnet-evm")

			hasViolation := []bool{
				importsPseudo,
				!inGraft && importsCoreth && inVMsEVM && !inEmulate,
				!inGraft && importsSubnetEVM && !inEmulate,
			}
			if slices.Contains(hasViolation, true) {
				violations = append(violations, fmt.Sprintf("File %q imports %q", file, importPath))
			}
		}
		return nil
	})

	require.NoErrorf(t, err, "filepath.Walk(%q)", root)
	require.Empty(t, violations, "import violations found:\n%s", strings.Join(violations, "\n"))
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
