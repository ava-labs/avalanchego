// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"path/filepath"
	"time"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/tests/load/agent"
	"github.com/ava-labs/avalanchego/tests/load/generator"
	"github.com/ava-labs/avalanchego/tests/load/issuer"
	"github.com/ava-labs/avalanchego/tests/load/orchestrate"
	"github.com/ava-labs/avalanchego/tests/load/tracker"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

const (
	keyDir = "./keys"
)

type Config struct {
	Endpoints   []string `json:"endpoints"`
	MaxFeeCap   int64    `json:"max-fee-cap"`
	MaxTipCap   int64    `json:"max-tip-cap"`
	Agents      uint     `json:"agents"`
	TxsPerAgent uint64   `json:"txs-per-agent"`
}

func Execute(ctx context.Context, config Config) error {
	keys, err := loadKeysFromDir(keyDir)
	if err != nil {
		return fmt.Errorf("loading keys: %w", err)
	}

	keys, err = ensureKeysNumber(keys, config.Agents)
	if err != nil {
		return fmt.Errorf("ensuring keys number: %w", err)
	}

	// Minimum to fund gas for all of the transactions for an address:
	minFundsPerAddr := new(big.Int).SetUint64(params.GWei * uint64(config.MaxFeeCap) * params.TxGas * config.TxsPerAgent)
	err = ensureMinimumFunds(ctx, config.Endpoints[0], keys, minFundsPerAddr)
	if err != nil {
		return fmt.Errorf("ensuring minimum funds: %w", err)
	}

	registry := prometheus.NewRegistry()
	metricsServer := tracker.NewMetricsServer("127.0.0.1:8082", registry)
	tracker := tracker.New(registry)

	agents := make([]*agent.Agent[*types.Transaction, common.Hash], config.Agents)
	for i := range agents {
		endpoint := config.Endpoints[i%len(config.Endpoints)]
		client, err := ethclient.DialContext(ctx, endpoint)
		if err != nil {
			return fmt.Errorf("dialing %s: %w", endpoint, err)
		}
		generator, err := generator.NewSelf(ctx, client,
			big.NewInt(config.MaxTipCap), big.NewInt(config.MaxFeeCap), keys[i])
		if err != nil {
			return fmt.Errorf("creating generator: %w", err)
		}
		address := ethcrypto.PubkeyToAddress(keys[i].PublicKey)
		issuer := issuer.New(client, tracker, address)
		agents[i] = agent.New[*types.Transaction, common.Hash](config.TxsPerAgent, generator, issuer, tracker)
	}

	metricsErrCh, err := metricsServer.Start()
	if err != nil {
		return fmt.Errorf("starting metrics server: %w", err)
	}

	orchestratorCtx, orchestratorCancel := context.WithCancel(ctx)
	defer orchestratorCancel()
	orchestrator := orchestrate.NewBurstOrchestrator(agents, time.Second)
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

func ensureKeysNumber(keys []*ecdsa.PrivateKey, target uint) ([]*ecdsa.PrivateKey, error) {
	for len(keys) < int(target) {
		newKey, err := ethcrypto.GenerateKey()
		if err != nil {
			return nil, fmt.Errorf("generating key: %w", err)
		}

		address := ethcrypto.PubkeyToAddress(newKey.PublicKey).Hex()
		filePath := filepath.Join(keyDir, address)
		err = ethcrypto.SaveECDSA(filePath, newKey)
		if err != nil {
			return nil, fmt.Errorf("saving key at index %d: %w", len(keys)-1, err)
		}
		keys = append(keys, newKey)
	}
	return keys, nil
}
