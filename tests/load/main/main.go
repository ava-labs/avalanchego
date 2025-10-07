// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"time"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/contracts"
)

const (
	blockchainID     = "C"
	metricsNamespace = "load"
	pollFrequency    = time.Millisecond
	testTimeout      = time.Minute
	defaultNodeCount = 5
)

var (
	flagVars *e2e.FlagVars

	devnetConfigPath string

	loadTimeoutArg     time.Duration
	firewoodEnabledArg bool
	numWorkersArg      int
)

func init() {
	flagVars = e2e.RegisterFlags(
		e2e.WithDefaultNodeCount(defaultNodeCount),
	)

	flag.StringVar(
		&devnetConfigPath,
		"devnet-config-path",
		"",
		"the file path for the devnet config",
	)

	flag.DurationVar(
		&loadTimeoutArg,
		"load-timeout",
		0,
		"the duration that the load test should run for",
	)
	flag.BoolVar(
		&firewoodEnabledArg,
		"firewood",
		false,
		"whether to use Firewood in Coreth",
	)
	flag.IntVar(
		&numWorkersArg,
		"num-workers",
		defaultNodeCount,
		"the number of workers to use for the load test",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("")
	tc := tests.NewTestContext(log)
	defer tc.RecoverAndExit()

	require := require.New(tc)

	ctx := tests.DefaultNotifyContext(0, tc.DeferCleanup)
	tc.SetDefaultContextParent(ctx)

	registry := prometheus.NewRegistry()
	metricsServer, err := tests.NewPrometheusServer(registry)
	require.NoError(err)
	tc.DeferCleanup(func() {
		require.NoError(metricsServer.Stop())
	})

	var workers []load.Worker
	if devnetConfigPath != "" {
		b, err := os.ReadFile(devnetConfigPath)
		require.NoError(err)

		var devnetConfig load.DevnetConfig
		require.NoError(json.Unmarshal(b, &devnetConfig))

		workers = load.ConnectNetwork(tc, metricsServer, devnetConfig)
	} else {
		workers = startNetwork(tc, metricsServer)
	}

	chainID, err := workers[0].Client.ChainID(ctx)
	require.NoError(err)

	tokenContract, err := newTokenContract(ctx, chainID, &workers[0], workers[1:])
	require.NoError(err)

	randomTest, err := load.NewRandomTest(
		ctx,
		chainID,
		&workers[0],
		rand.NewSource(time.Now().UnixMilli()),
		tokenContract,
	)
	require.NoError(err)

	generator, err := load.NewLoadGenerator(
		workers,
		chainID,
		metricsNamespace,
		registry,
		randomTest,
	)
	require.NoError(err)

	log.Info("starting load generator")

	generator.Run(ctx, log, loadTimeoutArg, testTimeout)
}

// startNetwork starts a new network and returns a list of workers who can
// interact with the network.
//
// Customization of the network and the number of workers can be set via flags.
func startNetwork(tc tests.TestContext, metricsServer *tests.PrometheusServer) []load.Worker {
	require := require.New(tc)

	numNodes, err := flagVars.NodeCount()
	require.NoError(err, "failed to get node count")

	nodes := tmpnet.NewNodesOrPanic(numNodes)

	keys, err := tmpnet.NewPrivateKeys(numWorkersArg)
	require.NoError(err)

	primaryChainConfigs := tmpnet.DefaultChainConfigs()
	if firewoodEnabledArg {
		primaryChainConfigs = newPrimaryChainConfigsWithFirewood()
	}
	network := &tmpnet.Network{
		Nodes:               nodes,
		PreFundedKeys:       keys,
		PrimaryChainConfigs: primaryChainConfigs,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)

	wsURIs, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
	require.NoError(err)

	monitoringConfigFilePath, err := tmpnet.WritePrometheusSDConfig("load-test", tmpnet.SDConfig{
		Targets: []string{metricsServer.Address()},
		Labels:  network.GetMonitoringLabels(),
	}, false)
	require.NoError(err, "failed to generate monitoring config file")

	tc.DeferCleanup(func() {
		require.NoError(
			os.Remove(monitoringConfigFilePath),
			"failed †o remove monitoring config file",
		)
	})

	workers := make([]load.Worker, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		workers[i] = load.Worker{
			PrivKey: keys[i].ToECDSA(),
			Client:  client,
		}
	}

	return workers
}

// newTokenContract deploys an instance of an ERC20 token and distributes the
// token supply evenly between the deployer and the recipients
func newTokenContract(
	ctx context.Context,
	chainID *big.Int,
	deployer *load.Worker,
	recipients []load.Worker,
) (*contracts.ERC20, error) {
	client := deployer.Client
	txOpts, err := bind.NewKeyedTransactorWithChainID(deployer.PrivKey, chainID)
	if err != nil {
		return nil, err
	}

	var (
		totalRecipients = big.NewInt(int64(len(recipients)) + 1)
		// assumes that token has 18 decimals
		recipientAmount = big.NewInt(1e18)
		totalSupply     = new(big.Int).Mul(totalRecipients, recipientAmount)
	)

	_, tx, contract, err := contracts.DeployERC20(txOpts, client, totalSupply)
	if err != nil {
		return nil, err
	}

	if _, err := bind.WaitDeployed(ctx, client, tx); err != nil {
		return nil, err
	}

	deployer.Nonce++

	for _, recipient := range recipients {
		tx, err := contract.Transfer(txOpts, crypto.PubkeyToAddress(recipient.PrivKey.PublicKey), recipientAmount)
		if err != nil {
			return nil, err
		}

		receipt, err := bind.WaitMined(ctx, client, tx)
		if err != nil {
			return nil, err
		}

		deployer.Nonce++

		if receipt.Status != types.ReceiptStatusSuccessful {
			return nil, fmt.Errorf("tx failed with status: %d", receipt.Status)
		}
	}

	return contract, nil
}

// newPrimaryChainConfigsWithFirewood extends the default primary chain configs
// by enabling Firewood on the C-Chain.
func newPrimaryChainConfigsWithFirewood() map[string]tmpnet.ConfigMap {
	primaryChainConfigs := tmpnet.DefaultChainConfigs()
	if _, ok := primaryChainConfigs[blockchainID]; !ok {
		primaryChainConfigs[blockchainID] = make(tmpnet.ConfigMap)
	}

	// firewoodConfig represents the minimum configuration required to enable
	// Firewood in Coreth.
	//
	// Ref: https://github.com/ava-labs/coreth/issues/1180
	firewoodConfig := tmpnet.ConfigMap{
		"state-scheme":       "firewood",
		"snapshot-cache":     0,
		"pruning-enabled":    true,
		"state-sync-enabled": false,
	}

	maps.Copy(primaryChainConfigs[blockchainID], firewoodConfig)
	return primaryChainConfigs
}
