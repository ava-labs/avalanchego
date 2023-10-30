// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// e2e implements the e2e tests.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet/local"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const (
	// A long default timeout used to timeout failed operations but
	// unlikely to induce flaking due to unexpected resource
	// contention.
	DefaultTimeout = 2 * time.Minute

	// Interval appropriate for network operations that should be
	// retried periodically but not too often.
	DefaultPollingInterval = 500 * time.Millisecond

	// Setting this env will disable post-test bootstrap
	// checks. Useful for speeding up iteration during test
	// development.
	SkipBootstrapChecksEnvName = "E2E_SKIP_BOOTSTRAP_CHECKS"

	// Validator start time must be a minimum of SyncBound from the
	// current time for validator addition to succeed, and adding 20
	// seconds provides a buffer in case of any delay in processing.
	DefaultValidatorStartTimeDiff = executor.SyncBound + 20*time.Second

	DefaultGasLimit = uint64(21000) // Standard gas limit

	// An empty string prompts the use of the default path which ensures a
	// predictable target for github's upload-artifact action.
	DefaultNetworkDir = ""

	// Directory used to store private networks (specific to a single test)
	// under the shared network dir.
	PrivateNetworksDirName = "private_networks"
)

// Env is used to access shared test fixture. Intended to be
// initialized by SynchronizedBeforeSuite.
var Env *TestEnvironment

type TestEnvironment struct {
	// The directory where the test network configuration is stored
	NetworkDir string
	// URIs used to access the API endpoints of nodes of the network
	URIs []testnet.NodeURI
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
func (te *TestEnvironment) GetRandomNodeURI() testnet.NodeURI {
	r := rand.New(rand.NewSource(time.Now().Unix())) //#nosec G404
	nodeURI := te.URIs[r.Intn(len(te.URIs))]
	tests.Outf("{{blue}} targeting node %s with URI: %s{{/}}\n", nodeURI.NodeID, nodeURI.URI)
	return nodeURI
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
	tests.Outf("{{blue}} allocated funded key(s): %+v{{/}}\n", keys)
	return keys
}

// Retrieve a funded key allocated for the caller's exclusive use.
func (te *TestEnvironment) AllocateFundedKey() *secp256k1.PrivateKey {
	return te.AllocateFundedKeys(1)[0]
}

// Create a new keychain with the specified number of test keys.
func (te *TestEnvironment) NewKeychain(count int) *secp256k1fx.Keychain {
	keys := te.AllocateFundedKeys(count)
	return secp256k1fx.NewKeychain(keys...)
}

// Create a new wallet for the provided keychain against the specified node URI.
// TODO(marun) Make this a regular function.
func (te *TestEnvironment) NewWallet(keychain *secp256k1fx.Keychain, nodeURI testnet.NodeURI) primary.Wallet {
	tests.Outf("{{blue}} initializing a new wallet for node %s with URI: %s {{/}}\n", nodeURI.NodeID, nodeURI.URI)
	baseWallet, err := primary.MakeWallet(DefaultContext(), &primary.WalletConfig{
		URI:          nodeURI.URI,
		AVAXKeychain: keychain,
		EthKeychain:  keychain,
	})
	te.require.NoError(err)
	return primary.NewWalletWithOptions(
		baseWallet,
		common.WithPostIssuanceFunc(
			func(id ids.ID) {
				tests.Outf(" issued transaction with ID: %s\n", id)
			},
		),
	)
}

// Create a new eth client targeting the specified node URI.
// TODO(marun) Make this a regular function.
func (te *TestEnvironment) NewEthClient(nodeURI testnet.NodeURI) ethclient.Client {
	tests.Outf("{{blue}} initializing a new eth client for node %s with URI: %s {{/}}\n", nodeURI.NodeID, nodeURI.URI)
	nodeAddress := strings.Split(nodeURI.URI, "//")[1]
	uri := fmt.Sprintf("ws://%s/ext/bc/C/ws", nodeAddress)
	client, err := ethclient.Dial(uri)
	te.require.NoError(err)
	return client
}

// Create a new private network that is not shared with other tests.
func (te *TestEnvironment) NewPrivateNetwork() testnet.Network {
	// Load the shared network to retrieve its path and exec path
	sharedNetwork, err := local.ReadNetwork(te.NetworkDir)
	te.require.NoError(err)

	// The private networks dir is under the shared network dir to ensure it
	// will be included in the artifact uploaded in CI.
	privateNetworksDir := filepath.Join(sharedNetwork.Dir, PrivateNetworksDirName)
	te.require.NoError(os.MkdirAll(privateNetworksDir, perms.ReadWriteExecute))

	return StartLocalNetwork(sharedNetwork.ExecPath, privateNetworksDir)
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
	require.NoError(ginkgo.GinkgoT(), testnet.WaitForHealthy(DefaultContext(), node))
}

// Sends an eth transaction, waits for the transaction receipt to be issued
// and checks that the receipt indicates success.
func SendEthTransaction(ethClient ethclient.Client, signedTx *types.Transaction) *types.Receipt {
	require := require.New(ginkgo.GinkgoT())

	txID := signedTx.Hash()
	tests.Outf(" sending eth transaction with ID: %s\n", txID)

	require.NoError(ethClient.SendTransaction(DefaultContext(), signedTx))

	// Wait for the receipt
	var receipt *types.Receipt
	Eventually(func() bool {
		var err error
		receipt, err = ethClient.TransactionReceipt(DefaultContext(), txID)
		if errors.Is(err, interfaces.NotFound) {
			return false // Transaction is still pending
		}
		require.NoError(err)
		return true
	}, DefaultTimeout, DefaultPollingInterval, "failed to see transaction acceptance before timeout")

	require.Equal(receipt.Status, types.ReceiptStatusSuccessful)
	return receipt
}

// Determines the suggested gas price for the configured client that will
// maximize the chances of transaction acceptance.
func SuggestGasPrice(ethClient ethclient.Client) *big.Int {
	gasPrice, err := ethClient.SuggestGasPrice(DefaultContext())
	require.NoError(ginkgo.GinkgoT(), err)
	// Double the suggested gas price to maximize the chances of
	// acceptance. Maybe this can be revisited pending resolution of
	// https://github.com/ava-labs/coreth/issues/314.
	gasPrice.Add(gasPrice, gasPrice)
	return gasPrice
}

// Helper simplifying use via an option of a gas price appropriate for testing.
func WithSuggestedGasPrice(ethClient ethclient.Client) common.Option {
	baseFee := SuggestGasPrice(ethClient)
	return common.WithBaseFee(baseFee)
}

// Verify that a new node can bootstrap into the network.
func CheckBootstrapIsPossible(network testnet.Network) {
	if len(os.Getenv(SkipBootstrapChecksEnvName)) > 0 {
		tests.Outf("{{yellow}}Skipping bootstrap check due to the %s env var being set", SkipBootstrapChecksEnvName)
		return
	}
	ginkgo.By("checking if bootstrap is possible with the current network state")
	node := AddEphemeralNode(network, testnet.FlagsMap{})
	WaitForHealthy(node)
}

// Start a local test-managed network with the provided avalanchego binary.
func StartLocalNetwork(avalancheGoExecPath string, networkDir string) *local.LocalNetwork {
	require := require.New(ginkgo.GinkgoT())

	network, err := local.StartNetwork(
		DefaultContext(),
		ginkgo.GinkgoWriter,
		networkDir,
		&local.LocalNetwork{
			LocalConfig: local.LocalConfig{
				ExecPath: avalancheGoExecPath,
			},
		},
		testnet.DefaultNodeCount,
		testnet.DefaultFundedKeyCount,
	)
	require.NoError(err)
	ginkgo.DeferCleanup(func() {
		tests.Outf("Shutting down network\n")
		require.NoError(network.Stop())
	})

	tests.Outf("{{green}}Successfully started network{{/}}\n")

	return network
}
