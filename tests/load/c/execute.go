// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/params"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/avalanchego/tests/load/c/generate"
	"github.com/ava-labs/avalanchego/tests/load/c/issue"
	"github.com/ava-labs/avalanchego/tests/load/c/listen"
	"github.com/ava-labs/avalanchego/tests/load/c/tracker"
	"github.com/ava-labs/avalanchego/tests/load/orchestrate"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type config struct {
	endpoints   []string
	maxFeeCap   int64
	maxTipCap   int64
	agents      uint
	txsPerAgent uint64
}

func execute(tc tests.TestContext, ctx context.Context, preFundedKeys []*secp256k1.PrivateKey, config config) error {
	logger := log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelInfo, true))
	log.SetDefault(logger)

	keys, err := fixKeysCount(preFundedKeys, int(config.agents))
	if err != nil {
		return fmt.Errorf("fixing keys count: %w", err)
	}

	// Minimum to fund gas for all of the transactions for an address:
	minFundsPerAddr := new(big.Int).SetUint64(params.GWei * uint64(config.maxFeeCap) * 1_000_000 * config.txsPerAgent)
	err = ensureMinimumFunds(tc, ctx, config.endpoints[0], keys, minFundsPerAddr)
	if err != nil {
		return fmt.Errorf("ensuring minimum funds: %w", err)
	}

	registry := prometheus.NewRegistry()
	metricsServer := tracker.NewMetricsServer("127.0.0.1:8082", registry)
	tracker := tracker.New(registry)

	agents := make([]*agent.Agent[*types.Transaction, common.Hash], config.agents)
	for i := range agents {
		endpoint := config.endpoints[i%len(config.endpoints)]
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("dialing %s: %w", endpoint, err)
		}
		generator, err := generate.NewOpCodeSimulator(
			ctx,
			client,
			big.NewInt(config.maxTipCap),
			big.NewInt(config.maxFeeCap),
			keys[i])
		if err != nil {
			return fmt.Errorf("creating generator: %w", err)
		}
		address := ethcrypto.PubkeyToAddress(keys[i].PublicKey)
		issuer := issue.New(client, tracker, address)
		listener := listen.New(client, tracker, config.txsPerAgent, address)
		agents[i] = agent.New[*types.Transaction, common.Hash](config.txsPerAgent, generator, issuer, listener, tracker)
	}

	metricsErrCh, err := metricsServer.Start()
	if err != nil {
		return fmt.Errorf("starting metrics server: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestrator := orchestrate.NewBurstOrchestrator(agents, time.Minute*5)
	orchestratorErrCh := make(chan error)
	go func() {
		orchestratorErrCh <- orchestrator.Execute(tc, orchestratorCtx)
	}()

	select {
	case err := <-orchestratorErrCh:
		tc.Log().Info("orchestrator finished")
		if err != nil {
			tc.Log().Info("orchestrator error", zap.Error(err))
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
