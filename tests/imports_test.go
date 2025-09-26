// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/packages"
)

// TestMustNotImport tests that we do not import certain packages that would configure libevm globally.
// This must not be done for packages that support both coreth and subnet-evm.
func TestMustNotImport(t *testing.T) {
	require := require.New(t)

	// These packages configure libevm globally by registering custom types and extras.
	illegalPaths := []string{
		"github.com/ava-labs/coreth/params",
		"github.com/ava-labs/coreth/plugin/evm/customtypes",
	}
	mustNotImport := map[string][]string{
		"tests/":            illegalPaths,
		"tests/antithesis/": illegalPaths,
		"tests/fixture/...": illegalPaths,
	}
	for packageName, forbiddenImports := range mustNotImport {
		packagePath := path.Join("github.com/ava-labs/avalanchego", packageName)
		imports, err := packages.GetDependencies(packagePath)
		require.NoError(err)

		for _, forbiddenImport := range forbiddenImports {
			require.NotContains(imports, forbiddenImport, "package %s must not import %s, check output of: go list -f '{{ .Deps }}' %q", packageName, forbiddenImport, packagePath)
		}
	}
}
