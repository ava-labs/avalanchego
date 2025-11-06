// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/packages"
)

// TestMustNotImport tests that we do not import certain packages (like coreth's customtypes package) that would configure libevm globals.
// Libevm panics if a package tries to register a custom type or extra after the globals have been configured (re-registration).
// E2E test packages are used both from Subnet-EVM and Coreth.
// Registering the libevm globals here in these packages does not necessarily break avalanchego e2e testing, but Subnet-EVM (or Coreth) E2E testing.
// So any illegal use here will only be a problem once Subnet-EVM or Coreth bumps the avalanchego version, breaking the release cycles of
// AvalancheGo and other repositories.
// Transitory imports are also checked with packages.GetDependencies.
func TestMustNotImport(t *testing.T) {
	require := require.New(t)

	// These packages configure libevm globally by registering custom types and extras.
	illegalPaths := []string{
		"github.com/ava-labs/avalanchego/graft/coreth/params",
		"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes",
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
