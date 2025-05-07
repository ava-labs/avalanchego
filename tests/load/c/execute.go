// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/tests/load"
	"github.com/ava-labs/avalanchego/tests/load/c/issuers"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type config struct {
	endpoints    []string
	maxFeeCap    int64
	agents       uint
	txsPerAgent  uint64
	issuePeriod  time.Duration
	finalTimeout time.Duration
}

func execute(ctx context.Context, preFundedKeys []*secp256k1.PrivateKey, config config) error {
	logger := logging.NewLogger("")

	keys, err := fixKeysCount(preFundedKeys, int(config.agents))
	if err != nil {
		return fmt.Errorf("fixing keys count: %w", err)
	}

	// Minimum to fund gas for all of the transactions for an address:
	minFundsPerAddr := big.NewInt(params.GWei)
	minFundsPerAddr = minFundsPerAddr.Mul(minFundsPerAddr, big.NewInt(config.maxFeeCap))
	minFundsPerAddr = minFundsPerAddr.Mul(minFundsPerAddr, new(big.Int).SetUint64(params.TxGas))
	minFundsPerAddr = minFundsPerAddr.Mul(minFundsPerAddr, new(big.Int).SetUint64(config.txsPerAgent))
	err = ensureMinimumFunds(ctx, config.endpoints[0], keys, minFundsPerAddr)
	if err != nil {
		return fmt.Errorf("ensuring minimum funds: %w", err)
	}

	registry := prometheus.NewRegistry()
	metricsServer := load.NewPrometheusServer("127.0.0.1:8082", registry)
	tracker, err := load.NewPrometheusTracker[*types.Transaction](registry, logger)
	if err != nil {
		return fmt.Errorf("creating tracker: %w", err)
	}

	agents := make([]load.Agent[*types.Transaction], config.agents)
	for i := range agents {
		endpoint := config.endpoints[i%len(config.endpoints)]
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("dialing %s: %w", endpoint, err)
		}
		issuer, err := issuers.NewSimple(ctx, client, tracker,
			big.NewInt(config.maxFeeCap), keys[i], config.issuePeriod)
		if err != nil {
			return fmt.Errorf("creating issuer: %w", err)
		}
		address := ethcrypto.PubkeyToAddress(keys[i].PublicKey)
		listener := NewListener(client, tracker, address)
		agents[i] = load.NewAgent(issuer, listener)
	}

	metricsErrCh, err := metricsServer.Start()
	if err != nil {
		return fmt.Errorf("starting metrics server: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestratorConfig := load.BurstOrchestratorConfig{
		TxsPerIssuer: config.txsPerAgent,
		Timeout:      config.finalTimeout,
	}
	orchestrator, err := load.NewBurstOrchestrator(agents, logger, orchestratorConfig)
	if err != nil {
		_ = metricsServer.Stop()
		return fmt.Errorf("creating orchestrator: %w", err)
	}

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
