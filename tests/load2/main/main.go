// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"flag"
	"math/big"
	"os"
	"time"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/contracts"
	"github.com/ava-labs/avalanchego/tests/load2"
)

const (
	blockchainID     = "C"
	metricsNamespace = "load"
	pollFrequency    = time.Millisecond
	testTimeout      = time.Minute
)

var (
	flagVars *e2e.FlagVars

	loadTimeout time.Duration
)

func init() {
	flagVars = e2e.RegisterFlags()

	flag.DurationVar(
		&loadTimeout,
		"load-timeout",
		0,
		"the duration that the load test should run for",
	)

	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger("")
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)

	numNodes, err := flagVars.NodeCount()
	require.NoError(err, "failed to get node count")

	nodes := tmpnet.NewNodesOrPanic(numNodes)

	keys, err := tmpnet.NewPrivateKeys(numNodes)
	require.NoError(err)
	network := &tmpnet.Network{
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)

	ctx := tests.DefaultNotifyContext(0, tc.DeferCleanup)
	wsURIs, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err)

	registry := prometheus.NewRegistry()
	metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry)
	metricsErrChan, err := metricsServer.Start()
	require.NoError(err)

	tc.DeferCleanup(func() {
		select {
		case err := <-metricsErrChan:
			require.NoError(err)
		default:
		}

		require.NoError(metricsServer.Stop(), "failed to stop metrics server")
	})

	monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(
		log,
		network.GetMonitoringLabels(),
	)
	require.NoError(err)

	tc.DeferCleanup(func() {
		require.NoError(os.Remove(monitoringConfigFilePath))
	})

	workers := make([]load2.Worker, len(keys))
	for i := range len(keys) {
		wsURI := wsURIs[i%len(wsURIs)]
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)

		workers[i] = load2.Worker{
			PrivKey: keys[i].ToECDSA(),
			Client:  client,
		}
	}

	chainID, err := workers[0].Client.ChainID(ctx)
	require.NoError(err)

	txOpts, err := bind.NewKeyedTransactorWithChainID(workers[0].PrivKey, chainID)
	require.NoError(err)

	_, tx, contract, err := contracts.DeployEVMLoadSimulator(txOpts, workers[0].Client)
	require.NoError(err)

	_, err = bind.WaitDeployed(ctx, workers[0].Client, tx)
	require.NoError(err)
	workers[0].Nonce++

	randomTest, err := createRandomTest(contract)
	require.NoError(err)

	generator, err := load2.NewLoadGenerator(
		workers,
		chainID,
		metricsNamespace,
		registry,
		randomTest,
	)
	require.NoError(err)

	generator.Run(tc, ctx, loadTimeout, testTimeout)
}

func createRandomTest(contract *contracts.EVMLoadSimulator) (load2.RandomTest, error) {
	count := big.NewInt(5)
	weightedTests := []load2.WeightedTest{
		{
			Test:   load2.ZeroTransferTest{},
			Weight: 100,
		},
		{
			Test: load2.ReadTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test: load2.WriteTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test: load2.StateModificationTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test: load2.HashingTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test: load2.MemoryTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test: load2.CallDepthTest{
				Contract: contract,
				Count:    count,
			},
			Weight: 100,
		},
		{
			Test:   load2.ContractCreationTest{Contract: contract},
			Weight: 100,
		},
		{
			Test: load2.PureComputeTest{
				Contract:      contract,
				NumIterations: count,
			},
			Weight: 100,
		},
		{
			Test: load2.LargeEventTest{
				Contract:  contract,
				NumEvents: count,
			},
			Weight: 100,
		},
		{
			Test:   load2.ExternalCallTest{Contract: contract},
			Weight: 100,
		},
	}

	return load2.NewRandomTest(weightedTests)
}
