// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package rpc converts an SAE VM into forms suitable for backing [ethapi],
// [tracers], and [filters] packages, and for providing other [rpc] namespaces
// (e.g. web3 and net).
package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/gasprice"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/txgossip"
)

// A Chain provides the consensus and execution state required to serve RPC
// requests.
type Chain interface {
	// Static and configuration
	Logger() logging.Logger
	Hooks() hook.Points
	ChainConfig() *params.ChainConfig

	// Consensus and block-building
	Peers() *p2p.Peers
	Mempool() *txgossip.Set
	blocks.Chain
	ChainContext() core.ChainContext

	// Execution results and replay
	saedb.StateDBOpener
	RecentReceipt(context.Context, common.Hash) (*saexec.Receipt, bool, error)
	NewBlock(eth *types.Block, parent, lastSettled *blocks.Block) (*blocks.Block, error)

	// Subscriptions
	SubscribeAcceptedBlocks(chan<- *blocks.Block) event.Subscription
	SubscribeChainEvent(chan<- core.ChainEvent) event.Subscription
	SubscribeChainHeadEvent(chan<- core.ChainHeadEvent) event.Subscription
	SubscribeLogsEvent(chan<- []*types.Log) event.Subscription
}

// Config controls which JSON-RPC namespaces are enabled and their resource
// limits.
type Config struct {
	BlocksPerBloomSection uint64
	EnableDBInspecting    bool
	EnableProfiling       bool
	DisableTracing        bool
	EVMTimeout            time.Duration
	GasCap                uint64
	TxFeeCap              float64 // 0 = no cap
}

// A Provider provides an [rpc.Server] along with the raw geth backends that
// handle the RPC requests.
type Provider struct {
	backend *backend
	server  *rpc.Server
	filter  *filters.FilterAPI
}

// New constructs a new [Provider].
func New(chain Chain, config Config) (*Provider, error) {
	price, err := gasprice.NewEstimator(&estimatorBackend{chain}, chain.Logger(), gasprice.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("gasprice.NewEstimator(...): %v", err)
	}

	chainIdx := chainIndexer{chain}
	override := bloomOverrider{chain}

	back := &backend{
		chain,
		config,
		// An empty account manager provides graceful errors for signing RPCs
		// (e.g. eth_sign) instead of nil-pointer panics. No actual account
		// functionality is expected.
		accounts.NewManager(&accounts.Config{}),
		price,
		chain.Mempool(),
		chainIdx,
		override,
		newBloomIndexer(
			// TODO(alarso16): if we are state syncing, we need to provide the
			// first block available to the indexer via
			// [core.ChainIndexer.AddCheckpoint].
			chain.DB(),
			chainIdx,
			override,
			config.BlocksPerBloomSection,
		),
	}

	filter := filters.NewFilterAPI(
		filters.NewFilterSystem(back, filters.Config{}),
		false, /*isLightClient*/
	)
	srv, err := back.server(filter)
	if err != nil {
		filters.CloseAPI(filter)
		return nil, errors.Join(err, back.close())
	}

	return &Provider{back, srv, filter}, nil
}

var _ io.Closer = (*Provider)(nil)

// Close releases all resources in use by the [GethBackends], and stops the
// [Provider.Server].
func (p *Provider) Close() error {
	filters.CloseAPI(p.filter)
	p.server.Stop()
	return p.backend.close()
}
