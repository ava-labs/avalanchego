// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	ctx := t.Context()

	network := NewDefaultNetwork("testnet")
	// Runtime configuration is required
	network.DefaultRuntimeConfig.Process = &ProcessRuntimeConfig{}
	// Validate round-tripping of primary subnet configuration
	network.PrimarySubnetConfig = ConfigMap{
		"validatorOnly": true,
	}
	require.NoError(network.EnsureDefaultConfig(ctx, logging.NoLog{}))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes(ctx))

	loadedNetwork, err := ReadNetwork(ctx, logging.NoLog{}, network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	require.Equal(network, loadedNetwork)
}
