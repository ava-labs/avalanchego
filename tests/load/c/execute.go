// Co// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"

	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/issuers"
	"github.com/ava-labs/avalanchego/tests/load/c/listener"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"

	ethcrypto "github.com/ava-labs/libevm/crypto"
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

func execute(ctx context.Context, preFundedKeys []*secp256k1.PrivateKey, config loadConfig, metrics *load.Metrics, logger logging.Logger) error {
	keys, err := fixKeysCount(preFundedKeys, int(config.agents))
	if err != nil {
		return fmt.Errorf("fixing keys count: %w", err)
	}

	err = distribute(ctx, config.endpoints[0], keys)
	if err != nil {
		return fmt.Errorf("ensuring minimum funds: %w", err)
	}

	agents, err := createAgents(ctx, config, keys, tracker)
	if err != nil {
		return fmt.Errorf("creating agents: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestratorConfig := load.NewOrchestratorConfig()
	orchestratorConfig.MinTPS = config.minTPS
	orchestratorConfig.MaxTPS = config.maxTPS
	orchestratorConfig.Step = config.step
	orchestratorConfig.TxRateMultiplier = 1.05
	orchestrator := load.NewOrchestrator(agents, tracker, logger, orchestratorConfig)

	return orchestrator.Execute(orchestratorCtx)
}

func fixKeysCount(preFundedKeys []*secp256k1.PrivateKey, target int) ([]*ecdsa.PrivateKey, error) {
	keys := make([]*ecdsa.PrivateKey, 0, target)
	for i := 0; i < min(target, len(preFundedKeys)); i++ {
		keys = append(keys, preFundedKeys[i].ToECDSA())
	}
	if len(keys) == target {
		return keys, nil
	}
	for i := len(keys); i < target; i++ {
		key, err := ethcrypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generating key at index %d: %w", i, err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// createAgents creates agents for the given configuration and keys.
// It creates them in parallel because creating issuers can sometimes take a while,
// and this adds up for many agents. For example, deploying the Opcoder contract
// takes a few seconds. Running the creation in parallel can reduce the time significantly.
func createAgents(ctx context.Context, config loadConfig, keys []*ecdsa.PrivateKey,
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
		go func(key *ecdsa.PrivateKey, endpoint string) {
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

func createAgent(ctx context.Context, endpoint string, key *ecdsa.PrivateKey,
	issuerType issuerType, tracker *load.Tracker[common.Hash], maxFeeCap int64,
) (load.Agent[common.Hash], error) {
	client, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		return load.Agent[common.Hash]{}, fmt.Errorf("dialing %s: %w", endpoint, err)
	}

	address := ethcrypto.PubkeyToAddress(key.PublicKey)
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
	maxFeeCap int64, nonce uint64, key *ecdsa.PrivateKey,
) (load.Issuer[common.Hash], error) {
	switch typ {
	case issuerSimple:
		return issuers.NewSimple(ctx, client, tracker,
			nonce, big.NewInt(maxFeeCap), key)
	case issuerOpcoder:
		return issuers.NewOpcoder(ctx, client, tracker,
			nonce, big.NewInt(maxFeeCap), key)
	default:
		return nil, fmt.Errorf("unknown issuer type %s", typ)
	}
}
