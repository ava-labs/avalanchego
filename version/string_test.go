// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionsGetString(t *testing.T) {
	versions := Versions{
		Application: "1",
		Database:    "2",
		RPCChainVM:  3,
		Go:          "5",
	}
	expectedVersions := fmt.Sprintf(
		"%s [database=%s, rpcchainvm=%d, go=%s]",
		versions.Application,
		versions.Database,
		versions.RPCChainVM,
		versions.Go,
	)
	require.Equal(t, expectedVersions, versions.String())

	versions.Commit = "eafd"
	expectedVersions = fmt.Sprintf(
		"%s [database=%s, rpcchainvm=%d, commit=%s, go=%s]",
		versions.Application,
		versions.Database,
		versions.RPCChainVM,
		versions.Commit,
		versions.Go,
	)
	require.Equal(t, expectedVersions, versions.String())
}
