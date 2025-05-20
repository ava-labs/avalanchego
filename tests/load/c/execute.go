// Co// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"fmt"
	"math/big"
	"os"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/issuers"
	"github.com/ava-labs/avalanchego/tests/load/c/listener"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type loadConfig struct {
	endpoints []string
	maxFeeCap int64
	agents    uint
	minTPS    int64
	maxTPS    int64
	step      int64
	issuer    issuerType
}

type issuerType string

const (
	issuerSimple  issuerType = "simple"
	issuerOpcoder issuerType = "opcoder"
)

func execute(ctx context.Context, keys []*secp256k1.PrivateKey, config loadConfig) error {
	logger := logging.NewLogger("", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Auto.ConsoleEncoder()))

	registry := prometheus.NewRegistry()
	metricsServer := load.NewPrometheusServer("127.0.0.1:8082", registry, logger)
	tracker, err := load.NewTracker[common.Hash](registry)
	if err != nil {
		return fmt.Errorf("creating tracker: %w", err)
	}

	agents, err := createAgents(ctx, config, keys, tracker)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}

	metricsErrCh, err := metricsServer.Start()
	if err != nil {
		return fmt.Errorf("starting metrics server: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestratorConfig := load.NewOrchestratorConfig()
	orchestratorConfig.MinTPS = config.minTPS
	orchestratorConfig.MaxTPS = config.maxTPS
	orchestratorConfig.Step = config.step
	orchestratorConfig.TxRateMultiplier = 1.05
	orchestrator := load.NewOrchestrator(agents, tracker, logger, orchestratorConfig)
	orchestratorErrCh := make(chan error)
	go func() {
		orchestratorErrCh <- orchestrator.Execute(orchestratorCtx)
	}()

	select {
	case err := <-orchestratorErrCh:
		if err != nil {
			_ = metricsServer.Stop()
			return fmt.Errorf("orchestrator error: %w", err)
		}
		err = metricsServer.Stop()
		if err != nil {
			return fmt.Errorf("stopping metrics server: %w", err)
		}
		return nil
	case err := <-metricsErrCh:
		orchestratorCancel()
		<-orchestratorErrCh
		return fmt.Errorf("metrics server error: %w", err)
	}
}

// createAgents creates agents for the given configuration and keys.
// It creates them in parallel because creating issuers can sometimes take a while,
// and this adds up for many agents. For example, deploying the Opcoder contract
// takes a few seconds. Running the creation in parallel can reduce the time significantly.
func createAgents(ctx context.Context, config loadConfig, keys []*secp256k1.PrivateKey,
	tracker *load.Tracker[common.Hash],
) ([]load.Agent[common.Hash], error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		agent load.Agent[common.Hash]
		err   error
	}
	ch := make(chan result)
	for i := range int(config.agents) {
		key := keys[i]
		endpoint := config.endpoints[i%len(config.endpoints)]
		go func(key *secp256k1.PrivateKey, endpoint string) {
			agent, err := createAgent(ctx, endpoint, key, config.issuer, tracker, config.maxFeeCap)
			ch <- result{agent: agent, err: err}
		}(key, endpoint)
	}

	var err error
	agents := make([]load.Agent[common.Hash], 0, int(config.agents))
	for range int(config.agents) {
		result := <-ch
		switch {
		case result.err == nil && err == nil: // no previous error or new error
			agents = append(agents, result.agent)
		case err != nil: // error already occurred
			continue
		case result.err != nil: // first error
			err = result.err
			cancel()
		default:
			panic("unreachable")
		}
	}

	if err != nil {
		return nil, err
	}
	return agents, nil
}

func createAgent(ctx context.Context, endpoint string, key *secp256k1.PrivateKey,
	issuerType issuerType, tracker *load.Tracker[common.Hash], maxFeeCap int64,
) (load.Agent[common.Hash], error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("dialing %s: %w", endpoint, err)
	}

	address := key.EthAddress()
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	issuer, err := createIssuer(ctx, issuerType,
		client, tracker, maxFeeCap, nonce, key)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("creating issuer: %w", err)
	}
	listener := listener.New(client, tracker, address, nonce)
	return load.NewAgent(issuer, listener), nil
}

func createIssuer(ctx context.Context, typ issuerType,
	client *ethclient.Client, tracker *load.Tracker[common.Hash],
	maxFeeCap int64, nonce uint64, key *secp256k1.PrivateKey,
) (load.Issuer[common.Hash], error) {
	switch typ {
	case issuerSimple:
		return issuers.NewSimple(ctx, client, tracker,
			nonce, big.NewInt(maxFeeCap), key.ToECDSA())
	case issuerOpcoder:
		return issuers.NewOpcoder(ctx, client, tracker,
			nonce, big.NewInt(maxFeeCap), key.ToECDSA())
	default:
		return nil, fmt.Errorf("unknown issuer type %s", typ)
	}
}
