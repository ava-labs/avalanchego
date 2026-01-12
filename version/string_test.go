// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionsGetString(t *testing.T) {
	versions := Versions{
		Application: "1",
		Database:    "2",
		RPCChainVM:  3,
		Commit:      "4",
		Go:          "5",
	}
	require.Equal(t, "1 [database=2, rpcchainvm=3, commit=4, go=5]", versions.String())
	versions.Commit = ""
	require.Equal(t, "1 [database=2, rpcchainvm=3, go=5]", versions.String())
}
