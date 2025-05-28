// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/listener"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type LoadConfig struct {
	Endpoints []string
	Agents    uint
	MinTPS    int64
	MaxTPS    int64
	Step      int64
}

func Execute(
	ctx context.Context,
	keys []*secp256k1.PrivateKey,
	config LoadConfig,
	metrics *load.Metrics,
	logger logging.Logger,
) error {
	tracker := load.NewTracker[common.Hash](metrics)
	agents, err := createAgents(ctx, config, keys, tracker)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestratorConfig := load.NewOrchestratorConfig()
	orchestratorConfig.MinTPS = config.MinTPS
	orchestratorConfig.MaxTPS = config.MaxTPS
	orchestratorConfig.Step = config.Step
	orchestratorConfig.TxRateMultiplier = 1.1
	orchestrator := load.NewOrchestrator(agents, tracker, logger, orchestratorConfig)

	return orchestrator.Execute(orchestratorCtx)
}

// createAgents creates agents for the given configuration and keys.
// It creates them in parallel because creating issuers can sometimes take a while,
// and this adds up for many agents. For example, deploying the Opcoder contract
// takes a few seconds. Running the creation in parallel can reduce the time significantly.
func createAgents(
	ctx context.Context,
	config LoadConfig,
	keys []*secp256k1.PrivateKey,
	tracker *load.Tracker[common.Hash],
) ([]load.Agent[common.Hash], error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		agent load.Agent[common.Hash]
		err   error
	}
	ch := make(chan result, config.Agents)
	for i := range int(config.Agents) {
		key := keys[i]
		endpoint := config.Endpoints[i%len(config.Endpoints)]
		go func(key *secp256k1.PrivateKey, endpoint string) {
			agent, err := createAgent(ctx, endpoint, key, tracker)
			ch <- result{agent: agent, err: err}
		}(key, endpoint)
	}

	agents := make([]load.Agent[common.Hash], 0, int(config.Agents))
	for range int(config.Agents) {
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
	blockNumber := (*big.Int)(nil)
	nonce, err := client.NonceAt(ctx, address, blockNumber)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("getting nonce for address %s: %w", address, err)
	}

	issuer, err := createIssuer(ctx, client, nonce, key.ToECDSA())
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("creating issuer: %w", err)
	}
	listener := listener.New(client, tracker, address, nonce)
	return load.NewAgent(issuer, listener), nil
}
