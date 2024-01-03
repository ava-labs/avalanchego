// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/interfaces"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
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

	DefaultGasLimit = uint64(21000) // Standard gas limit

	// An empty string prompts the use of the default path which ensures a
	// predictable target for github's upload-artifact action.
	DefaultNetworkDir = ""

	// Directory used to store private networks (specific to a single test)
	// under the shared network dir.
	PrivateNetworksDirName = "private_networks"
)

// Create a new wallet for the provided keychain against the specified node URI.
func NewWallet(keychain *secp256k1fx.Keychain, nodeURI tmpnet.NodeURI) primary.Wallet {
	tests.Outf("{{blue}} initializing a new wallet for node %s with URI: %s {{/}}\n", nodeURI.NodeID, nodeURI.URI)
	baseWallet, err := primary.MakeWallet(DefaultContext(), &primary.WalletConfig{
		URI:          nodeURI.URI,
		AVAXKeychain: keychain,
		EthKeychain:  keychain,
	})
	require.NoError(ginkgo.GinkgoT(), err)
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
func NewEthClient(nodeURI tmpnet.NodeURI) ethclient.Client {
	tests.Outf("{{blue}} initializing a new eth client for node %s with URI: %s {{/}}\n", nodeURI.NodeID, nodeURI.URI)
	nodeAddress := strings.Split(nodeURI.URI, "//")[1]
	uri := fmt.Sprintf("ws://%s/ext/bc/C/ws", nodeAddress)
	client, err := ethclient.Dial(uri)
	require.NoError(ginkgo.GinkgoT(), err)
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

// Adds an ephemeral node intended to be used by a single test.
func AddEphemeralNode(network *tmpnet.Network, flags tmpnet.FlagsMap) *tmpnet.Node {
	require := require.New(ginkgo.GinkgoT())

	node, err := network.AddEphemeralNode(DefaultContext(), ginkgo.GinkgoWriter, flags)
	require.NoError(err)

	ginkgo.DeferCleanup(func() {
		tests.Outf("shutting down ephemeral node %q\n", node.NodeID)
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})
	return node
}

// Wait for the given node to report healthy.
func WaitForHealthy(node *tmpnet.Node) {
	// Need to use explicit context (vs DefaultContext()) to support use with DeferCleanup
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	require.NoError(ginkgo.GinkgoT(), tmpnet.WaitForHealthy(ctx, node))
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

// Verify that a new node can bootstrap into the network. This function is safe to call
// from `Teardown` by virtue of not depending on ginkgo.DeferCleanup.
func CheckBootstrapIsPossible(network *tmpnet.Network) {
	require := require.New(ginkgo.GinkgoT())

	if len(os.Getenv(SkipBootstrapChecksEnvName)) > 0 {
		tests.Outf("{{yellow}}Skipping bootstrap check due to the %s env var being set", SkipBootstrapChecksEnvName)
		return
	}
	ginkgo.By("checking if bootstrap is possible with the current network state")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// Node stop will be initiated if the node is not started without error.
	node, err := network.AddEphemeralNode(ctx, ginkgo.GinkgoWriter, tmpnet.FlagsMap{})
	require.NoError(err)

	err = tmpnet.WaitForHealthy(ctx, node)

	// Ensure the node is stopped regardless of whether bootstrap succeeded. Use a new
	// context in case the previous one timed out waiting for the node to become healthy.
	ctx, cancel = context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	require.NoError(node.Stop(ctx))

	// Check that the node became healthy within timout
	require.NoError(err)
}

// Start a temporary network with the provided avalanchego binary.
func StartNetwork(avalancheGoExecPath string, networkDir string) *tmpnet.Network {
	require := require.New(ginkgo.GinkgoT())

	network, err := tmpnet.StartNetwork(
		DefaultContext(),
		ginkgo.GinkgoWriter,
		networkDir,
		&tmpnet.Network{
			NodeRuntimeConfig: tmpnet.NodeRuntimeConfig{
				AvalancheGoPath: avalancheGoExecPath,
			},
		},
		tmpnet.DefaultNodeCount,
		tmpnet.DefaultPreFundedKeyCount,
	)
	require.NoError(err)
	ginkgo.DeferCleanup(func() {
		tests.Outf("Shutting down network\n")
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		require.NoError(network.Stop(ctx))
	})

	tests.Outf("{{green}}Successfully started network{{/}}\n")

	return network
}
