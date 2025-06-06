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

	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	blockchainID = "C"
	// invariant: nodesCount >= 5
	nodesCount       = 5
	sendersPerNode   = 1
	sendersCount     = nodesCount * sendersPerNode
	logPrefix        = "avalanchego-load-test"
	metricsNamespace = "load"
)

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags()
	flag.Parse()
}

func main() {
	log := tests.NewDefaultLogger(logPrefix)
	tc := tests.NewTestContext(log)
	defer tc.Cleanup()

	require := require.New(tc)
	ctx := context.Background()

	nodes := tmpnet.NewNodesOrPanic(nodesCount)

	keys, err := tmpnet.NewPrivateKeys(sendersCount)
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
	metrics, err := load.NewMetrics(metricsNamespace, registry)
	require.NoError(err, "failed to register load metrics")

	metricsServer := load.NewPrometheusServer("127.0.0.1:0", registry)
	merticsErrCh, err := metricsServer.Start()
	require.NoError(err, "failed to start load metrics server")

	monitoringConfigFilePath, err := metricsServer.GenerateMonitoringConfig(network.GetMonitoringLabels())
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

	endpoints, err := tmpnet.GetNodeWebsocketURIs(ctx, network.Nodes, blockchainID, tc.DeferCleanup)
	require.NoError(err, "failed †o get node websocket URIs")

	w := &workload{
		endpoints:  endpoints,
		numSenders: sendersCount,
	}

	require.NoError(
		w.run(ctx, log, network.PreFundedKeys, metrics),
		"failed to execute load test",
	)
}

type workload struct {
	endpoints  []string
	numSenders uint
}

func (w *workload) run(
	ctx context.Context,
	log logging.Logger,
	keys []*secp256k1.PrivateKey,
	metrics *load.Metrics,
) error {
	tracker := load.NewTracker(metrics)
	senders, builders, err := w.createSendersAndBuilders(ctx, keys)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}

	orchestrator := load.NewOrchestrator(senders, builders, tracker, log)

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	return orchestrator.Run(ctx)
}

// createSendersAndBuilders creates senders and builders for the given configuration and keys.
// It creates them in parallel because creating builders can sometimes take a while,
// and this adds up for many builders. For example, deploying the Opcoder contract
// takes a few seconds. Running the creation in parallel can reduce the time significantly.
func (w *workload) createSendersAndBuilders(
	ctx context.Context,
	keys []*secp256k1.PrivateKey,
) ([]*load.Sender, []load.Builder, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		sender  *load.Sender
		builder load.Builder
		err     error
	}

	ch := make(chan result, w.numSenders)
	wg := sync.WaitGroup{}
	for i := range int(w.numSenders) {
		key := keys[i]
		endpoint := w.endpoints[i%len(w.endpoints)]
		wg.Add(1)
		go func(key *secp256k1.PrivateKey, endpoint string) {
			defer wg.Done()
			sender, builder, err := createSenderAndBuilder(ctx, endpoint, key)
			ch <- result{sender: sender, builder: builder, err: err}
		}(key, endpoint)
	}

	defer func() {
		wg.Wait()
		close(ch)
	}()

	senders := make([]*load.Sender, 0, len(keys))
	builders := make([]load.Builder, 0, len(keys))
	for range int(w.numSenders) {
		select {
		case result := <-ch:
			// exit immediately if we hit an error
			if result.err != nil {
				return nil, nil, result.err
			}
			senders = append(senders, result.sender)
			builders = append(builders, result.builder)
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}

	return senders, builders, nil
}

func createSenderAndBuilder(
	ctx context.Context,
	endpoint string,
	key *secp256k1.PrivateKey,
) (*load.Sender, load.Builder, error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial %s: %w", endpoint, err)
	}

	address := key.EthAddress()
	nonce, err := client.NonceAt(ctx, address, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	issuer, err := c.NewIssuer(ctx, client, nonce, key.ToECDSA())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create issuer: %w", err)
	}

	sender := load.NewSender(client)
	return sender, issuer, nil
}
