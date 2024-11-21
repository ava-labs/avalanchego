// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestNetworkSerialization(t *testing.T) {
	require := require.New(t)

	tmpDir := t.TempDir()

	network := NewDefaultNetwork("testnet")
	// TODO(marun) Remove when zap.NewNop() is possible
	log := logging.NewLogger(
		"",
		logging.NewWrappedCore(
			logging.Verbo,
			os.Stdout,
			zapcore.NewConsoleEncoder(zapcore.EncoderConfig{}),
		),
	)
	require.NoError(network.EnsureDefaultConfig(log, "/path/to/avalanche/go", ""))
	require.NoError(network.Create(tmpDir))
	// Ensure node runtime is initialized
	require.NoError(network.readNodes())

	loadedNetwork, err := ReadNetwork(network.Dir)
	require.NoError(err)
	for _, key := range loadedNetwork.PreFundedKeys {
		// Address() enables comparison with the original network by
		// ensuring full population of a key's in-memory representation.
		_ = key.Address()
	}
	require.Equal(network, loadedNetwork)
}
