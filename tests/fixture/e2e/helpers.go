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

	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/testenv"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethereum "github.com/ava-labs/libevm"
)

const (
	DefaultTimeout = tests.DefaultTimeout

	DefaultPollingInterval = tmpnet.DefaultPollingInterval

	// Setting this env will disable post-test bootstrap
	// checks. Useful for speeding up iteration during test
	// development.
	SkipBootstrapChecksEnvName = "E2E_SKIP_BOOTSTRAP_CHECKS"

	DefaultValidatorStartTimeDiff = tmpnet.DefaultValidatorStartTimeDiff

	DefaultGasLimit = uint64(21000) // Standard gas limit

	// Directory used to store private networks (specific to a single test)
	// under the shared network dir.
	PrivateNetworksDirName = "private_networks"
)

var (
	// testenv helpers for creating and managing test environments
	GetEnv                    = testenv.GetEnv
	NewTestEnvironment        = testenv.NewTestEnvironment
	InitSharedTestEnvironment = testenv.InitSharedTestEnvironment
	StartNetwork              = testenv.StartNetwork
)

// NewPrivateKey returns a new private key.
func NewPrivateKey(tc tests.TestContext) *secp256k1.PrivateKey {
	key, err := secp256k1.NewPrivateKey()
	require.NoError(tc, err)
	return key
}

// Create a new wallet for the provided keychain against the specified node URI.
func NewWallet(tc tests.TestContext, keychain *secp256k1fx.Keychain, nodeURI tmpnet.NodeURI) *primary.Wallet {
	log := tc.Log()
	log.Info("initializing a new wallet",
		zap.Stringer("nodeID", nodeURI.NodeID),
		zap.String("URI", nodeURI.URI),
	)
	baseWallet, err := primary.MakeWallet(
		tc.DefaultContext(),
		nodeURI.URI,
		keychain,
		keychain,
		primary.WalletConfig{},
	)
	require.NoError(tc, err)
	wallet := primary.NewWalletWithOptions(
		baseWallet,
		common.WithIssuanceHandler(func(r common.IssuanceReceipt) {
			log.Info("issued transaction",
				zap.String("chainAlias", r.ChainAlias),
				zap.Stringer("txID", r.TxID),
				zap.Duration("duration", r.Duration),
			)
		}),
		common.WithConfirmationHandler(func(r common.ConfirmationReceipt) {
			log.Info("confirmed transaction",
				zap.String("chainAlias", r.ChainAlias),
				zap.Stringer("txID", r.TxID),
				zap.Duration("totalDuration", r.TotalDuration),
				zap.Duration("confirmationDuration", r.ConfirmationDuration),
			)
		}),
		// Reducing the default from 100ms speeds up detection of tx acceptance
		common.WithPollFrequency(10*time.Millisecond),
	)
	OutputWalletBalances(tc, wallet)
	return wallet
}

// OutputWalletBalances outputs the X-Chain and P-Chain balances of the provided wallet.
func OutputWalletBalances(tc tests.TestContext, wallet *primary.Wallet) {
	_, _ = GetWalletBalances(tc, wallet)
}

// GetWalletBalances retrieves the X-Chain and P-Chain balances of the provided wallet.
func GetWalletBalances(tc tests.TestContext, wallet *primary.Wallet) (uint64, uint64) {
	require := require.New(tc)
	var (
		xWallet  = wallet.X()
		xBuilder = xWallet.Builder()
		pWallet  = wallet.P()
		pBuilder = pWallet.Builder()
	)
	xBalances, err := xBuilder.GetFTBalance()
	require.NoError(err, "failed to fetch X-chain balances")
	pBalances, err := pBuilder.GetBalance()
	require.NoError(err, "failed to fetch P-chain balances")
	var (
		xContext    = xBuilder.Context()
		avaxAssetID = xContext.AVAXAssetID
		xAVAX       = xBalances[avaxAssetID]
		pAVAX       = pBalances[avaxAssetID]
	)
	tc.Log().Info("wallet balances in nAVAX",
		zap.Uint64("xChain", xAVAX),
		zap.Uint64("pChain", pAVAX),
	)
	return xAVAX, pAVAX
}

// Create a new eth client targeting the specified node URI.
func NewEthClient(tc tests.TestContext, nodeURI tmpnet.NodeURI) *ethclient.Client {
	tc.Log().Info("initializing a new eth client",
		zap.Stringer("nodeID", nodeURI.NodeID),
		zap.String("URI", nodeURI.URI),
	)
	nodeAddress := strings.Split(nodeURI.URI, "//")[1]
	uri := fmt.Sprintf("ws://%s/ext/bc/C/ws", nodeAddress)
	client, err := ethclient.Dial(uri)
	require.NoError(tc, err)
	return client
}

// Adds an ephemeral node intended to be used by a single test.
func AddEphemeralNode(tc tests.TestContext, network *tmpnet.Network, node *tmpnet.Node) *tmpnet.Node {
	require := require.New(tc)

	require.NoError(network.StartNode(tc.DefaultContext(), node))

	tc.DeferCleanup(func() {
		tc.Log().Info("shutting down ephemeral node",
			zap.Stringer("nodeID", node.NodeID),
		)
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})
	return node
}

// Wait for the given node to report healthy.
func WaitForHealthy(t require.TestingT, node *tmpnet.Node) {
	// Need to use explicit context (vs DefaultContext()) to support use with DeferCleanup
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()
	require.NoError(t, node.WaitForHealthy(ctx))
}

// Sends an eth transaction and waits for the transaction receipt from the
// execution of the transaction.
func SendEthTransaction(tc tests.TestContext, ethClient *ethclient.Client, signedTx *types.Transaction) *types.Receipt {
	require := require.New(tc)

	txID := signedTx.Hash()
	tc.Log().Info("sending eth transaction",
		zap.Stringer("txID", txID),
	)

	require.NoError(ethClient.SendTransaction(tc.DefaultContext(), signedTx))

	// Wait for the receipt
	var receipt *types.Receipt
	tc.Eventually(func() bool {
		var err error
		receipt, err = ethClient.TransactionReceipt(tc.DefaultContext(), txID)
		if errors.Is(err, ethereum.NotFound) {
			return false // Transaction is still pending
		}
		require.NoError(err)
		return true
	}, DefaultTimeout, DefaultPollingInterval, "failed to see transaction acceptance before timeout")

	tc.Log().Info("eth transaction accepted",
		zap.Stringer("txID", txID),
		zap.Uint64("gasUsed", receipt.GasUsed),
		zap.Stringer("gasPrice", receipt.EffectiveGasPrice),
		zap.Stringer("blockNumber", receipt.BlockNumber),
	)
	return receipt
}

// Determines the suggested gas price for the configured client that will
// maximize the chances of transaction acceptance.
func SuggestGasPrice(tc tests.TestContext, ethClient *ethclient.Client) *big.Int {
	gasPrice, err := ethClient.SuggestGasPrice(tc.DefaultContext())
	require.NoError(tc, err)

	tc.Log().Info("suggested gas price",
		zap.Stringer("price", gasPrice),
	)

	// Double the suggested gas price to maximize the chances of
	// acceptance. Maybe this can be revisited pending resolution of
	// https://github.com/ava-labs/coreth/issues/314.
	gasPrice.Add(gasPrice, gasPrice)
	return gasPrice
}

// Helper simplifying use via an option of a gas price appropriate for testing.
func WithSuggestedGasPrice(tc tests.TestContext, ethClient *ethclient.Client) common.Option {
	baseFee := SuggestGasPrice(tc, ethClient)
	return common.WithBaseFee(baseFee)
}

// Verify that a new node can bootstrap into the network. If the check wasn't skipped,
// the node will be returned to the caller.
func CheckBootstrapIsPossible(tc tests.TestContext, network *tmpnet.Network) *tmpnet.Node {
	require := require.New(tc)

	if len(os.Getenv(SkipBootstrapChecksEnvName)) > 0 {
		tc.Log().Info("skipping bootstrap check due to env var being set",
			zap.String("envVar", SkipBootstrapChecksEnvName),
		)
		return nil
	}
	tc.By("checking if bootstrap is possible with the current network state")

	// Ensure all subnets are bootstrapped
	subnetIDs := make([]string, len(network.Subnets))
	for i, subnet := range network.Subnets {
		subnetIDs[i] = subnet.SubnetID.String()
	}
	flags := tmpnet.FlagsMap{
		config.TrackSubnetsKey: strings.Join(subnetIDs, ","),
	}

	node := tmpnet.NewEphemeralNode(flags)
	require.NoError(network.StartNode(tc.DefaultContext(), node))
	// StartNode will initiate node stop if an error is encountered during start,
	// so no further cleanup effort is required if an error is seen here.

	// Register a cleanup to ensure the node is stopped at the end of the test
	tc.DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		require.NoError(node.Stop(ctx))
	})

	// Check that the node becomes healthy within timeout
	require.NoError(node.WaitForHealthy(tc.DefaultContext()))

	// Ensure that the primary validators are still healthy
	for _, node := range network.Nodes {
		if node.IsEphemeral {
			continue
		}
		healthy, err := node.IsHealthy(tc.DefaultContext())
		require.NoError(err)
		require.True(healthy, "primary validator %s is not healthy", node.NodeID)
	}

	return node
}

// NewPChainFeeCalculatorFromContext returns either a static or dynamic fee
// calculator depending on the provided context.
func NewPChainFeeCalculatorFromContext(context *builder.Context) fee.Calculator {
	if context.GasPrice != 0 {
		return fee.NewDynamicCalculator(context.ComplexityWeights, context.GasPrice)
	}
	return fee.NewSimpleCalculator(0)
}

// GetRepoRootPath strips the provided suffix from the current working
// directory. If the test binary is executed from the root of the repo, the
// result will be the repo root.
func GetRepoRootPath(suffix string) (string, error) {
	// - When executed via a test binary, the working directory will be wherever
	// the binary is executed from, but scripts should require execution from
	// the repo root.
	//
	// - When executed via ginkgo (nicer for development + supports
	// parallel execution) the working directory will always be the
	// target path (e.g. [repo root]./tests/bootstrap/e2e) and getting the repo
	// root will require stripping the target path suffix.
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(cwd, suffix), nil
}
