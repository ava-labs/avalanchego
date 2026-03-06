// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/sae"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/graft/evm/utils/rpc"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"

	avadb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	atomicvm "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// SinceGenesis is a harness around an [sae.VM], providing an `Initialize`
// method that treats the chain as being asynchronous since genesis.
type SinceGenesis struct {
	*sae.VM // created by [SinceGenesis.Initialize]
	hooks   *hooks
	config  sae.Config

	ctx          *snow.Context
	mempool      *txpool.Mempool
	pushGossiper *avalanchegossip.PushGossiper[*atomic.Tx]
	acceptedTxs  *state.AtomicRepository

	lastWaitForEvent utils.Atomic[time.Time]
}

// NewSinceGenesis constructs a new [SinceGenesis].
func NewSinceGenesis(c sae.Config) *SinceGenesis {
	return &SinceGenesis{
		config: c,
	}
}

// Initialize initializes the VM.
func (vm *SinceGenesis) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	_ []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	db := rawdb.NewDatabase(evmdb.New(avaDB))
	tdb := triedb.NewDatabase(db, vm.config.TrieDBConfig)

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", genesis, err)
	}

	{
		c := genesis.Config
		u := snowCtx.NetworkUpgrades

		c.HomesteadBlock = big.NewInt(0)
		c.DAOForkBlock = big.NewInt(0)
		c.DAOForkSupport = true
		c.EIP150Block = big.NewInt(0)
		c.EIP155Block = big.NewInt(0)
		c.EIP158Block = big.NewInt(0)
		c.ByzantiumBlock = big.NewInt(0)
		c.ConstantinopleBlock = big.NewInt(0)
		c.PetersburgBlock = big.NewInt(0)
		c.IstanbulBlock = big.NewInt(0)
		c.MuirGlacierBlock = big.NewInt(0)
		c.BerlinBlock = big.NewInt(0)
		c.LondonBlock = big.NewInt(0)
		c.ShanghaiTime = utils.PointerTo(uint64(u.DurangoTime.Unix()))
		c.CancunTime = utils.PointerTo(uint64(u.EtnaTime.Unix()))
	}

	config, _, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
	}

	txs := txpool.NewTxs()
	hooks := &hooks{
		blockBuilder{
			potentialTxs: txs,
		},
		snowCtx.Log,
	}
	inner, err := sae.NewVM(ctx, hooks, vm.config, snowCtx, config, db, genesis.ToBlock(), appSender)
	if err != nil {
		return err
	}
	vm.VM = inner
	vm.hooks = hooks
	vm.ctx = snowCtx

	metrics := prometheus.NewRegistry()
	if err := snowCtx.Metrics.Register("coreth", metrics); err != nil {
		return fmt.Errorf("failed to register metrics: %w", err)
	}

	{
		mempool, err := txpool.NewMempool(txs, metrics, func(tx *atomic.Tx) error {
			// TODO: Implement me
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to initialize mempool: %w", err)
		}
		vm.mempool = mempool
	}

	{
		gossipHandler, pullGossiper, pushGossiper, err := avalanchegossip.NewSystem(
			snowCtx.NodeID,
			vm.Network,
			nil, // TODO: Expose from SAE
			vm.mempool,
			&atomic.TxMarshaller{},
			avalanchegossip.SystemConfig{
				Log:       snowCtx.Log,
				Registry:  metrics,
				Namespace: "tx_gossip",
				HandlerID: p2p.AtomicTxGossipHandlerID,
			},
		)
		if err != nil {
			return fmt.Errorf("failed to initialize atomic gossip system: %w", err)
		}

		// TODO: Register these
		_ = gossipHandler
		_ = pullGossiper
		_ = pushGossiper
	}

	return nil
}

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits for the next event from the VM.
func (vm *SinceGenesis) WaitForEvent(ctx context.Context) (common.Message, error) {
	defer func() {
		vm.lastWaitForEvent.Set(time.Now())
	}()

	sinceLastCall := time.Since(vm.lastWaitForEvent.Get())
	timeToWait := waitForEventDelay - sinceLastCall
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(timeToWait):
		return vm.VM.WaitForEvent(ctx)
	}
}

const avaxHTTPExtensionPath = "/avax"

func (vm *SinceGenesis) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	m, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}

	avaxAPI, err := rpc.NewHandler("avax", &atomicvm.AvaxAPI{
		Context:      vm.ctx,
		Mempool:      vm.mempool,
		PushGossiper: vm.pushGossiper,
		AcceptedTxs:  vm.acceptedTxs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register service for AVAX API due to %w", err)
	}

	m[avaxHTTPExtensionPath] = avaxAPI
	return m, nil
}

// Shutdown gracefully closes the VM.
func (vm *SinceGenesis) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
