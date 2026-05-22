// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package cchain implements the C-Chain VM atop [sae.VM]. It composes the
// C-Chain block-building hooks, the cross-chain transaction pool, and the avax
// JSON-RPC service that ingests Export and Import transactions.
package cchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"

	avadb "github.com/ava-labs/avalanchego/database"
)

// VM wraps an [sae.VM] with the cross-chain pieces specific to the C-Chain.
type VM struct {
	*sae.VM // created by [VM.Initialize]

	// gossip frequencies are configurable to speed up testing.
	pullGossipPeriod time.Duration
	pushGossipPeriod time.Duration

	ctx          *snow.Context
	state        *state.State
	txpool       *txpool.Txpool
	pushGossiper *gossip.PushGossiper[*gossipTx]

	// onClose are executed in reverse order during [VM.Shutdown]. If a resource
	// depends on another resource, it MUST be added AFTER the resource it
	// depends on.
	onClose []func(context.Context) error
}

var ethDBPrefix = []byte("ethdb")

// Initialize initializes the VM.
func (v *VM) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) (retErr error) {
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, v.Shutdown(ctx))
		}
	}()

	v.ctx = snowCtx

	// TODO(StephenButtolph): Allow minimal user configuration via configBytes.
	_ = configBytes

	// [prefixdb.NewNested] is used because coreth used to be run as a plugin.
	// This meant that the database's prefix was not compacted, because the
	// provided database was wrapped by the rpcchainvm.
	ethDB := rawdb.NewDatabase(database.New(prefixdb.NewNested(ethDBPrefix, avaDB)))
	trieDBConfig := triedb.HashDefaults
	trieDB := triedb.NewDatabase(ethDB, trieDBConfig)

	// TODO(StephenButtolph): Replace this with Coreth's genesis format.
	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("unmarshalling genesis: %w", err)
	}
	chainConfig, _, err := core.SetupGenesisBlock(ethDB, trieDB, genesis)
	if err != nil {
		return fmt.Errorf("setting up genesis block: %w", err)
	}

	v.state, err = state.New(snowCtx, avaDB)
	if err != nil {
		return fmt.Errorf("creating cchain state: %w", err)
	}
	v.onClose = append(v.onClose, func(context.Context) error {
		return v.state.Close()
	})

	pendingTxs := txpool.NewPending()
	hooks := newHooks(
		snowCtx,
		v.state,
		pendingTxs,
	)
	mempoolConfig := legacypool.DefaultConfig
	// Treat all transactions equally regardless of submission source — no
	// preferential admission or pricing for locally-submitted txs.
	mempoolConfig.NoLocals = true
	saeConfig := sae.Config{
		MempoolConfig: mempoolConfig,
		DBConfig: saedb.Config{
			TrieDBConfig: trieDBConfig,
		},
	}
	v.VM, err = sae.NewVM(ctx, hooks, saeConfig, snowCtx, chainConfig, ethDB, genesis.ToBlock(), appSender)
	if err != nil {
		return fmt.Errorf("creating SAE VM: %w", err)
	}
	v.onClose = append(v.onClose, v.VM.Shutdown)

	const maxTxPoolSize = 1024
	v.txpool, err = txpool.New(snowCtx, chainConfig, pendingTxs, v.VM, maxTxPoolSize)
	if err != nil {
		return fmt.Errorf("creating txpool: %w", err)
	}
	v.onClose = append(v.onClose, func(context.Context) error {
		v.txpool.Close()
		return nil
	})

	reg, err := metrics.MakeAndRegister(snowCtx.Metrics, "cchain")
	if err != nil {
		return fmt.Errorf("making metrics: %w", err)
	}
	bloomMetrics, err := bloom.NewMetrics("gossip_bloom", reg)
	if err != nil {
		return fmt.Errorf("creating gossip bloom metrics: %w", err)
	}
	gossipSet, err := gossip.NewBloomSet(
		newGossipTxPool(v.txpool),
		gossip.BloomSetConfig{
			Metrics: bloomMetrics,
		},
	)
	if err != nil {
		return fmt.Errorf("creating gossip bloom set: %w", err)
	}
	gossipHandler, pullGossiper, pushGossiper, err := gossip.NewSystem(
		snowCtx.NodeID,
		v.Network,
		v.ValidatorPeers,
		gossipSet,
		gossipMarshaller{},
		gossip.SystemConfig{
			Log:           snowCtx.Log,
			Registry:      reg,
			Namespace:     "gossip",
			HandlerID:     p2p.AtomicTxGossipHandlerID,
			RequestPeriod: v.pullGossipPeriod,
		},
	)
	if err != nil {
		return fmt.Errorf("creating cross-chain tx gossip system: %w", err)
	}
	v.pushGossiper = pushGossiper

	if err := v.VM.AddHandler(p2p.AtomicTxGossipHandlerID, gossipHandler); err != nil {
		return fmt.Errorf("registering cross-chain tx gossip handler: %w", err)
	}

	gossipCtx, cancelGossip := context.WithCancel(context.Background())
	var gossipWG sync.WaitGroup
	gossipWG.Go(func() {
		gossip.Every(gossipCtx, snowCtx.Log, pullGossiper, v.pullGossipPeriod)
	})
	gossipWG.Go(func() {
		gossip.Every(gossipCtx, snowCtx.Log, pushGossiper, v.pushGossipPeriod)
	})
	v.onClose = append(v.onClose, func(context.Context) error {
		cancelGossip()
		gossipWG.Wait()
		return nil
	})

	return nil
}

const (
	avaxServiceName       = "avax"
	avaxHTTPExtensionPath = "/" + avaxServiceName
)

// CreateHandlers returns the HTTP handlers exposed by the underlying SAE VM
// augmented with the avax service at [avaxHTTPExtensionPath].
func (v *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	m, err := v.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating SAE handlers: %w", err)
	}

	service, err := newService(v.ctx, v.txpool, v.pushGossiper, v.state)
	if err != nil {
		return nil, fmt.Errorf("creating avax service: %w", err)
	}
	handler, err := rpc.NewHandler(avaxServiceName, service)
	if err != nil {
		return nil, fmt.Errorf("creating avax RPC handler: %w", err)
	}

	m[avaxHTTPExtensionPath] = handler
	return m, nil
}

// WaitForEvent waits for a transaction to be in the txpool or for the SAE VM to
// produce an event.
func (v *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	// TODO(StephenButtolph): Do not busy loop with [common.PendingTxs]. The
	// txpools are cleared after block execution, so we may still have
	// transactions in the txpool while blocks containing those transactions are
	// processing.

	// TODO(StephenButtolph): Wait until the minimum block delay has passed.

	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		msg common.Message
		err error
	}
	results := make(chan result, 2)
	go func() {
		defer cancel()
		msg, err := v.VM.WaitForEvent(ctx)
		results <- result{msg, err}
	}()
	go func() {
		defer cancel()
		err := v.txpool.AwaitTxs(ctx)
		results <- result{common.PendingTxs, err}
	}()

	r := <-results
	return r.msg, r.err
}

// Shutdown releases every resource allocated by [VM.Initialize] in reverse
// order.
//
// It is idempotent and safe to call after a partially-failed [VM.Initialize].
func (v *VM) Shutdown(ctx context.Context) error {
	errs := make([]error, len(v.onClose))
	for i, f := range slices.Backward(v.onClose) {
		errs[i] = f(ctx)
	}
	v.onClose = nil
	return errors.Join(errs...)
}
