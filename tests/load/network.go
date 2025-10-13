// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"fmt"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

// DevnetConfig holds the configuration for connecting to an existing devnet.
// It specifies the private keys to use for load generation and the WebSocket
// URIs of the nodes to connect to.
type DevnetConfig struct {
	PrivateKeys []string `json:"private-keys"`
	NodeWsURIs  []string `json:"node-ws-uris"`
}

// ParseKeys converts the string representation of private keys into their
// concrete types (*secp256k1.PrivateKeys). Returns an error if any key fails to parse.
func (d DevnetConfig) ParseKeys() ([]*secp256k1.PrivateKey, error) {
	keys := make([]*secp256k1.PrivateKey, len(d.PrivateKeys))
	for i, pk := range d.PrivateKeys {
		key := new(secp256k1.PrivateKey)
		if err := key.UnmarshalText([]byte(pk)); err != nil {
			return nil, fmt.Errorf("failed to unmarshal private key: %w", err)
		}

		keys[i] = key
	}

	return keys, nil
}

// ConnectNetwork connects to an existing network and returns a list of workers
// that can interact with the network.
func ConnectNetwork(tc tests.TestContext, devnetConfig DevnetConfig) []Worker {
	require := require.New(tc)

	keys, err := devnetConfig.ParseKeys()
	require.NoError(err)

	wsURIs := devnetConfig.NodeWsURIs
	workers := make([]Worker, len(keys))
	for i, key := range keys {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		nonce, err := client.NonceAt(tc.DefaultContext(), key.EthAddress(), nil)
		require.NoError(err)

		workers[i] = Worker{
			PrivKey: key.ToECDSA(),
			Client:  client,
			Nonce:   nonce,
		}
	}

	return workers
}
