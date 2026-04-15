// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyCommandRegistryMatchesIntegrationCoverage(t *testing.T) {
	coveredCommands := map[string]struct{}{
		"version":          {},
		"create":           {},
		"delete":           {},
		"get":              {},
		"get-state":        {},
		"replace-comments": {},
		"upsert-comment":   {},
		"delete-state":     {},
		"update-body":      {},
	}

	for _, spec := range proxyCommandRegistry() {
		_, ok := coveredCommands[spec.name]
		require.Truef(t, ok, "proxy integration coverage missing command %q", spec.name)
		delete(coveredCommands, spec.name)
	}

	require.Empty(t, coveredCommands, "proxy integration coverage has commands missing from the registry")
}
