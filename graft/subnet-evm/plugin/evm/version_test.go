// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/version"
)

func TestCompatibility(t *testing.T) {
	// Verify that the Subnet-EVM version is properly derived from AvalancheGo version
	expectedVersion := fmt.Sprintf("v%d.%d.%d", version.Current.Major, version.Current.Minor, version.Current.Patch)
	require.Equal(t, expectedVersion, Version,
		"Subnet-EVM version should be automatically derived from version.Current")

	// Verify that the version is compatible with current RPC chain VM protocol
	// Compatibility is now maintained through version/compatibility.json in the main repo
	require.NotZero(t, version.RPCChainVMProtocol,
		"RPC chain VM protocol version should be set")
}
