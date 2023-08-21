// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

const (
	// Enough for primary.NewWallet to fetch initial UTXOs.
	DefaultWalletCreationTimeout = 5 * time.Second

	// Defines default tx confirmation timeout.
	// Enough for test/custom networks.
	DefaultConfirmTxTimeout = 20 * time.Second

	// A long default timeout used to timeout failed operations but
	// unlikely to induce flaking due to unexpected resource
	// contention.
	DefaultTimeout = 2 * time.Minute

	// Interval appropriate for network operations that should be
	// retried periodically but not too often.
	DefaultPollingInterval = 500 * time.Millisecond
)

// Env is used to access shared test fixture. Intended to be
// initialized by SynchronizedBeforeSuite.
var Env *TestEnvironment

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []string
	// The URI used to access the http server that allocates test data
	TestDataServerURI string

	require *require.Assertions
}

func InitTestEnvironment(envBytes []byte) {
	require := require.New(ginkgo.GinkgoT())
	require.Nil(Env, "env already initialized")
	Env = &TestEnvironment{
		require: require,
	}
	require.NoError(json.Unmarshal(envBytes, Env))
}

// Retrieve a random URI to naively attempt to spread API load across
// nodes.
func (te *TestEnvironment) GetRandomNodeURI() string {
	r := rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
	return te.URIs[r.Intn(len(te.URIs))]
}

// Retrieve the network to target for testing.
func (te *TestEnvironment) GetNetwork() testnet.Network {
	network, err := local.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)
	return network
}

// Retrieve the specified number of funded keys allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKeys(count int) []*secp256k1.PrivateKey {
	keys, err := fixture.AllocateFundedKeys(te.TestDataServerURI, count)
	te.require.NoError(err)
	return keys
}

// Retrieve a funded key allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKey() *secp256k1.PrivateKey {
	return te.AllocateFundedKeys(1)[0]
}

// Create a new keychain with the specified number of test keys.
func (te *TestEnvironment) NewKeychain(count int) *secp256k1fx.Keychain {
	tests.Outf("{{blue}} initializing keychain with %d keys {{/}}\n", count)
	keys := te.AllocateFundedKeys(count)
	return secp256k1fx.NewKeychain(keys...)
}

// Create a new wallet for the provided keychain.
func (te *TestEnvironment) NewWallet(keychain *secp256k1fx.Keychain) primary.Wallet {
	tests.Outf("{{blue}} initializing a new wallet {{/}}\n")
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	wallet, err := primary.MakeWallet(ctx, &primary.WalletConfig{
		URI:          te.GetRandomNodeURI(),
		AVAXKeychain: keychain,
		EthKeychain:  keychain,
	})
	te.require.NoError(err)
	return wallet
}
