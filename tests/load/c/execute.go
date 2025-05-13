// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/issuers"
	"github.com/ava-labs/avalanchego/tests/load/c/listener"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type config struct {
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

func execute(ctx context.Context, preFundedKeys []*secp256k1.PrivateKey, config config) error {
	logger := logging.NewLogger("", logging.NewWrappedCore(logging.Info, os.Stdout, logging.Auto.ConsoleEncoder()))

	keys, err := fixKeysCount(preFundedKeys, int(config.agents))
	if err != nil {
		return fmt.Errorf("fixing keys count: %w", err)
	}

	err = distribute(ctx, config.endpoints[0], keys)
	if err != nil {
		return fmt.Errorf("ensuring minimum funds: %w", err)
	}

	registry := prometheus.NewRegistry()
	metricsServer := load.NewPrometheusServer("127.0.0.1:8082", registry, logger)
	tracker, err := load.NewTracker[common.Hash](registry, logger)
	if err != nil {
		return fmt.Errorf("creating tracker: %w", err)
	}

	agents := make([]load.Agent[common.Hash], config.agents)
	for i := range agents {
		endpoint := config.endpoints[i%len(config.endpoints)]
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("dialing %s: %w", endpoint, err)
		}

		address := ethcrypto.PubkeyToAddress(keys[i].PublicKey)
		blockNumber := (*big.Int)(nil)
		nonce, err := client.NonceAt(ctx, address, blockNumber)
		if err != nil {
			return fmt.Errorf("getting nonce for address %s: %w", address, err)
		}

		issuer, err := createIssuer(ctx, config.issuer,
			client, tracker, config.maxFeeCap, nonce, keys[i])
		if err != nil {
			return fmt.Errorf("creating issuer: %w", err)
		}
		listener := listener.New(client, tracker, address, nonce)
		agents[i] = load.NewAgent(issuer, listener)
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
	orchestratorConfig.SustainedTime = 10 * time.Second
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
