// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/ethclient"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	// Enough for primary.NewWallet to fetch initial UTXOs.
	DefaultWalletCreationTimeout = 5 * time.Second

	// Defines default tx confirmation timeout.
	// Enough for test/custom networks.
	DefaultConfirmTxTimeout = 20 * time.Second

	// This interval should represent the upper bound of the time
	// required to start a new node on a local test network.
	DefaultNodeStartTimeout = 20 * time.Second

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

// Create a new wallet for the provided keychain against a random node URI.
func (te *TestEnvironment) NewWallet(keychain *secp256k1fx.Keychain) primary.Wallet {
	return te.NewWalletForURI(keychain, te.GetRandomNodeURI())
}

// Create a new wallet for the provided keychain against the specified node URI.
func (te *TestEnvironment) NewWalletForURI(keychain *secp256k1fx.Keychain, uri string) primary.Wallet {
	tests.Outf("{{blue}} initializing a new wallet {{/}}\n")
	wallet, err := primary.MakeWallet(DefaultContext(), &primary.WalletConfig{
		URI:          uri,
		AVAXKeychain: keychain,
		EthKeychain:  keychain,
	})
	te.require.NoError(err)
	return wallet
}

// Create a new eth client targeting a random node.
func (te *TestEnvironment) NewEthClient() ethclient.Client {
	return te.NewEthClientForURI(te.GetRandomNodeURI())
}

// Create a new eth client targeting the specified node URI.
func (te *TestEnvironment) NewEthClientForURI(nodeURI string) ethclient.Client {
	nodeAddress := strings.Split(nodeURI, "//")[1]
	uri := fmt.Sprintf("ws://%s/ext/bc/C/ws", nodeAddress)
	client, err := ethclient.Dial(uri)
	te.require.NoError(err)
	return client
}

// Helper simplifying use of a timed context by canceling the context on ginkgo teardown.
func ContextWithTimeout(duration time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	ginkgo.DeferCleanup(cancel)
	return ctx
}

// Helper simplifying use of a timed context configured with the default timeout.
func DefaultContext() context.Context {
	return ContextWithTimeout(DefaultTimeout)
}

// Helper simplifying use via an option of a timed context configured with the default timeout.
func WithDefaultContext() common.Option {
	return common.WithContext(DefaultContext())
}

// Re-implementation of testify/require.Eventually that is compatible with ginkgo. testify's
// version calls the condition function with a goroutine and ginkgo assertions don't work
// properly in goroutines.
func Eventually(condition func() bool, waitFor time.Duration, tick time.Duration, msg string) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), waitFor)
	defer cancel()
	for !condition() {
		select {
		case <-ctx.Done():
			require.Fail(ginkgo.GinkgoT(), msg)
		case <-ticker.C:
		}
	}
}

// Add an ephemeral node that is only intended to be used by a single test. Its ID and
// URI are not intended to be returned from the Network instance to minimize
// accessibility from other tests.
func AddEphemeralNode(network testnet.Network, flags testnet.FlagsMap) testnet.Node {
	require := require.New(ginkgo.GinkgoT())

	node, err := network.AddEphemeralNode(ginkgo.GinkgoWriter, flags)
	require.NoError(err)

	// Ensure node is stopped on teardown. It's configuration is not removed to enable
	// collection in CI to aid in troubleshooting failures.
	ginkgo.DeferCleanup(func() {
		tests.Outf("Shutting down ephemeral node %s\n", node.GetID())
		require.NoError(node.Stop())
	})

	return node
}

// Wait for the given node to report healthy.
func WaitForHealthy(node testnet.Node) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	require.NoError(ginkgo.GinkgoT(), testnet.WaitForHealthy(ctx, node))
}
