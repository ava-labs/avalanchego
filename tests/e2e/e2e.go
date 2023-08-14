// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"math/rand"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
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
var Env TestEnvironment

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []string
	// The URI used to access the http server that allocates test data
	TestDataServerURI string
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
	require.NoError(ginkgo.GinkgoT(), err)
	return network
}

// Retrieve the specified number of funded keys allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKeys(count int) []*secp256k1.PrivateKey {
	keys, err := fixture.AllocateFundedKeys(te.TestDataServerURI, count)
	require.NoError(ginkgo.GinkgoT(), err)
	return keys
}

// Retrieve a funded key allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKey() *secp256k1.PrivateKey {
	return te.AllocateFundedKeys(1)[0]
}
