// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/packages"
)

func TestMustNotImport(t *testing.T) {
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
		fullPkgPath := path.Join(sourceRepo, packageName)
		imports, err := packages.GetDependencies(fullPkgPath)
		require.NoError(t, err)

		for _, forbiddenImport := range forbiddenImports {
			fullForbiddenImport := path.Join(targetRepo, forbiddenImport)
			require.NotContains(t, imports, fullForbiddenImport, "package %s must not import %s, check output of go list -f '{{ .Deps }}' \"%s\" ", packageName, fullForbiddenImport, fullPkgPath)
		}
	}
}
