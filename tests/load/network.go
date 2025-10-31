// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"github.com/ava-labs/libevm/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

type WorkerConfig struct {
	PrivateKey string `json:"private-key"`
	NodeWsURI  string `json:"node-ws-uri"`
}

// NetworkConfig holds the configuration for connecting to an existing devnet.
// It specifies the private keys to use for load generation and the WebSocket
// URIs of the nodes to connect to.
type NetworkConfig struct {
	WorkerConfigs []WorkerConfig `json:"worker-configs"`
}

// ConnectNetwork connects to an existing network and returns a list of workers
// who can interact with the network.
func ConnectNetwork(tc tests.TestContext, networkCfg NetworkConfig) []Worker {
	require := require.New(tc)

	workers := make([]Worker, len(networkCfg.WorkerConfigs))

	ctx := tc.GetDefaultContextParent()
	for i, workerCfg := range networkCfg.WorkerConfigs {
		rawKey := workerCfg.PrivateKey
		pk := new(secp256k1.PrivateKey)
		require.NoError(pk.UnmarshalText([]byte(rawKey)))

		client, err := ethclient.Dial(workerCfg.NodeWsURI)
		require.NoError(err)

		nonce, err := client.NonceAt(ctx, pk.EthAddress(), nil)
		require.NoError(err)

		workers[i] = Worker{
			PrivKey: pk.ToECDSA(),
			Nonce:   nonce,
			Client:  client,
		}
	}

	return workers
}
