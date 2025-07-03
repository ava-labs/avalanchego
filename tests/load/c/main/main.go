// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c"
	"github.com/ava-labs/avalanchego/tests/load/c/listener"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	blockchainID = "C"
	// invariant: nodesCount >= 5
	nodesCount    = 5
	agentsPerNode = 1
	agentsCount   = nodesCount * agentsPerNode
	logPrefix     = "avalanchego-load-test"
)

var flagVars *flags.FlagVars

func init() {
	flagVars = flags.RegisterFlags()
	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger(logPrefix)
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)
	ctx := context.Background()

	nodes := tmpnet.NewNodesOrPanic(nodesCount)

	keys, err := tmpnet.NewPrivateKeys(agentsCount)
	require.NoError(err)
	network := &tmpnet.Network{
		Owner:         "avalanchego-load-test",
		Nodes:         nodes,
		PreFundedKeys: keys,
	}

	e2e.NewTestEnvironment(tc, flagVars, network)
	tc.DeferCleanup(func() {
		require.NoError(network.Stop(ctx), "failed to stop network")
	})

	registry := prometheus.NewRegistry()
	metrics, err := load.NewMetrics(registry)
	require.NoError(err, "failed to register load metrics")

	metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry)
	merticsErrCh, err := metricsServer.Start()
	require.NoError(err, "failed to start load metrics server")

	monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(log, network.GetMonitoringLabels())
	require.NoError(err, "failed to generate monitoring config file")

	tc.DeferCleanup(func() {
		select {
		case err := <-merticsErrCh:
			require.NoError(err, "metrics server exited with error")
		default:
			require.NoError(metricsServer.Stop(), "failed to stop metrics server")
		}
	})

	tc.DeferCleanup(func() {
		require.NoError(
			os.Remove(monitoringConfigFilePath),
			"failed †o remove monitoring config file",
		)
	})

	endpoints, err := tmpnet.GetNodeWebsocketURIs(network.Nodes, blockchainID)
	require.NoError(err, "failed †o get node websocket URIs")

	w := &workload{
		endpoints: endpoints,
		numAgents: agentsPerNode,
		minTPS:    10,
		maxTPS:    50,
		step:      10,
	}

	require.NoError(
		w.run(ctx, log, network.PreFundedKeys, metrics),
		"failed to execute load test",
	)
}

type workload struct {
	endpoints []string
	numAgents uint
	minTPS    int64
	maxTPS    int64
	step      int64
}

func (w *workload) run(
	ctx context.Context,
	log logging.Logger,
	keys []*secp256k1.PrivateKey,
	metrics *load.Metrics,
) error {
	tracker := load.NewTracker[common.Hash](metrics)
	agents, err := w.createAgents(ctx, keys, tracker)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}

	orchestratorConfig := load.OrchestratorConfig{
		MaxTPS:           w.maxTPS,
		MinTPS:           w.minTPS,
		Step:             w.step,
		TxRateMultiplier: 1.1,
		SustainedTime:    20 * time.Second,
		MaxAttempts:      3,
		Terminate:        true,
	}

	orchestrator := load.NewOrchestrator(agents, tracker, log, orchestratorConfig)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	return orchestrator.Execute(ctx)
}

// createAgents creates agents for the given configuration and keys.
// It creates them in parallel because creating issuers can sometimes take a while,
// and this adds up for many agents. For example, deploying the Opcoder contract
// takes a few seconds. Running the creation in parallel can reduce the time significantly.
func (w *workload) createAgents(
	ctx context.Context,
	keys []*secp256k1.PrivateKey,
	tracker *load.Tracker[common.Hash],
) ([]load.Agent[common.Hash], error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		agent load.Agent[common.Hash]
		err   error
	}

	ch := make(chan result, w.numAgents)
	wg := sync.WaitGroup{}
	for i := range int(w.numAgents) {
		key := keys[i]
		endpoint := w.endpoints[i%len(w.endpoints)]
		wg.Add(1)
		go func(key *secp256k1.PrivateKey, endpoint string) {
			defer wg.Done()
			agent, err := createAgent(ctx, endpoint, key, tracker)
			ch <- result{agent: agent, err: err}
		}(key, endpoint)
	}

	defer func() {
		wg.Wait()
		close(ch)
	}()

	agents := make([]load.Agent[common.Hash], 0, int(w.numAgents))
	for range int(w.numAgents) {
		select {
		case result := <-ch:
			// exit immediately if we hit an error
			if result.err != nil {
				return nil, result.err
			}
			agents = append(agents, result.agent)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return agents, nil
}

func createAgent(
	ctx context.Context,
	endpoint string,
	key *secp256k1.PrivateKey,
	tracker *load.Tracker[common.Hash],
) (load.Agent[common.Hash], error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("dialing %s: %w", endpoint, err)
	}

	address := key.EthAddress()
	nonce, err := client.NonceAt(ctx, address, nil)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	issuer, err := c.NewIssuer(ctx, client, nonce, key.ToECDSA())
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("creating issuer: %w", err)
	}
	listener := listener.New(client, tracker, address, nonce)
	return load.NewAgent(issuer, listener), nil
}
