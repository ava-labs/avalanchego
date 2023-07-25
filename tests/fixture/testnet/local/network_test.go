// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNetworkSerialization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)

	network := &LocalNetwork{Dir: tmpDir}
	require.NoError(t, network.PopulateLocalNetworkConfig(1337, 1, 1))
	require.NoError(t, network.WriteAll())

	loadedNetwork, err := ReadNetwork(tmpDir)
	require.NoError(t, err)
	for _, key := range loadedNetwork.FundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	require.Equal(t, network, loadedNetwork)
}
