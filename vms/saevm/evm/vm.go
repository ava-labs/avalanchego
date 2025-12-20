// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	avalanchedb "github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	atomicstate "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/txpool"
	atomicvm "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/config"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/gossip"
	"github.com/ava-labs/avalanchego/graft/coreth/utils/rpc"
	"github.com/ava-labs/avalanchego/network/p2p"
	avalanchegossip "github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	corethdb "github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	sae "github.com/ava-labs/strevm"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	atomicMempoolSize     = 4096 // number of transactions
	atomicGossipNamespace = "atomic_tx_gossip"
	avaxEndpoint          = "/avax"

	atomicRegossipFrequency = 30 * time.Second
	atomicPushFrequency     = 100 * time.Millisecond
	atomicPullFrequency     = 1 * time.Second
)

var atomicTxMarshaller = &atomic.TxMarshaller{}

type VM struct {
	*sae.VM                    // Populated by [vm.Initialize]
	ctx        context.Context // Cancelled when onShutdown is called
	onShutdown context.CancelFunc
	wg         sync.WaitGroup

	chainContext *snow.Context

	atomicMempool *txpool.Mempool
	gossipMetrics avalanchegossip.Metrics
	pushGossiper  *avalanchegossip.PushGossiper[*atomic.Tx]
	acceptedTxs   *atomicstate.AtomicRepository
}

func (vm *VM) Initialize(
	ctx context.Context,
	chainContext *snow.Context,
	db avalanchedb.Database,
	genesisBytes []byte,
	configBytes []byte,
	_ []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	ethDB := rawdb.NewDatabase(corethdb.New(db))

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return err
	}
	sdb := state.NewDatabase(ethDB)
	chainConfig, genesisHash, err := core.SetupGenesisBlock(ethDB, sdb.TrieDB(), genesis)
	if err != nil {
		return err
	}

	batch := ethDB.NewBatch()
	// Being both the "head" and "finalized" block is a requirement of [Config].
	rawdb.WriteHeadBlockHash(batch, genesisHash)
	rawdb.WriteFinalizedBlockHash(batch, genesisHash)
	if err := batch.Write(); err != nil {
		return err
	}

	mempoolTxs := txpool.NewTxs(chainContext, atomicMempoolSize)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}

	vm.VM, err = sae.New(
		ctx,
		sae.Config{
			Hooks: &hooks{
				ctx:         chainContext,
				chainConfig: chainConfig,
				mempool:     mempoolTxs,
			},
			ChainConfig: chainConfig,
			DB:          ethDB,
			LastSynchronousBlock: sae.LastSynchronousBlock{
				Hash:        genesisHash,
				Target:      acp176.MinTargetPerSecond,
				ExcessAfter: 0,
			},
			SnowCtx:   chainContext,
			AppSender: appSender,
		},
	)
	if err != nil {
		return err
	}
	vm.ctx, vm.onShutdown = context.WithCancel(context.Background())
	vm.chainContext = chainContext

	metrics := prometheus.NewRegistry()
	vm.atomicMempool, err = txpool.NewMempool(mempoolTxs, metrics, vm.verifyTxAtTip)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}

	vm.gossipMetrics, err = avalanchegossip.NewMetrics(metrics, atomicGossipNamespace)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx gossip metrics: %w", err)
	}

	vm.pushGossiper, err = avalanchegossip.NewPushGossiper[*atomic.Tx](
		atomicTxMarshaller,
		vm.atomicMempool,
		vm.P2PValidators,
		vm.Network.NewClient(p2p.AtomicTxGossipHandlerID, vm.P2PValidators),
		vm.gossipMetrics,
		avalanchegossip.BranchingFactor{
			StakePercentage: .9,
			Validators:      100,
		},
		avalanchegossip.BranchingFactor{
			Validators: 10,
		},
		config.PushGossipDiscardedElements,
		config.TxGossipTargetMessageSize,
		atomicRegossipFrequency,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize atomic tx push gossiper: %w", err)
	}

	vm.acceptedTxs, err = atomicstate.NewAtomicTxRepository(
		versiondb.New(memdb.New()), // TODO
		atomic.Codec,
		0, // TODO
	)
	if err != nil {
		return fmt.Errorf("failed to create atomic repository: %w", err)
	}

	return nil
}

// TODO: Implement me
func (*VM) verifyTxAtTip(*atomic.Tx) error {
	return nil
}

func (vm *VM) SetState(ctx context.Context, state snow.State) error {
	if state != snow.NormalOp {
		return nil
	}

	return vm.registerAtomicMempoolGossip()
}

func (vm *VM) registerAtomicMempoolGossip() error {
	{
		// TODO: Don't make a new registry
		metrics := prometheus.NewRegistry()
		handler, err := gossip.NewTxGossipHandler[*atomic.Tx](
			vm.chainContext.Log,
			atomicTxMarshaller,
			vm.atomicMempool,
			vm.gossipMetrics,
			config.TxGossipTargetMessageSize,
			config.TxGossipThrottlingPeriod,
			config.TxGossipRequestsPerPeer,
			vm.P2PValidators,
			metrics,
			atomicGossipNamespace,
		)
		if err != nil {
			return fmt.Errorf("failed to initialize atomic tx gossip handler: %w", err)
		}

		// By registering the handler, we allow inbound network traffic for the
		// [p2p.AtomicTxGossipHandlerID] protocol.
		if err := vm.Network.AddHandler(p2p.AtomicTxGossipHandlerID, handler); err != nil {
			return fmt.Errorf("failed to add atomic tx gossip handler: %w", err)
		}
	}

	// Start push gossip to disseminate any transactions issued by this node to
	// a large percentage of the network.
	{
		vm.wg.Add(1)
		go func() {
			avalanchegossip.Every(vm.ctx, vm.chainContext.Log, vm.pushGossiper, atomicPushFrequency)
			vm.wg.Done()
		}()
	}

	// Start pull gossip to ensure this node quickly learns of transactions that
	// have already been distributed to a large percentage of the network.
	{
		pullGossiper := avalanchegossip.NewPullGossiper[*atomic.Tx](
			vm.chainContext.Log,
			atomicTxMarshaller,
			vm.atomicMempool,
			vm.Network.NewClient(p2p.AtomicTxGossipHandlerID, vm.P2PValidators),
			vm.gossipMetrics,
			config.TxGossipPollSize,
		)

		pullGossiperWhenValidator := &avalanchegossip.ValidatorGossiper{
			Gossiper:   pullGossiper,
			NodeID:     vm.chainContext.NodeID,
			Validators: vm.P2PValidators,
		}

		vm.wg.Add(1)
		go func() {
			avalanchegossip.Every(vm.ctx, vm.chainContext.Log, pullGossiperWhenValidator, atomicPullFrequency)
			vm.wg.Done()
		}()
	}

	return nil
}

func (vm *VM) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	apis, err := vm.VM.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}
	avaxAPI, err := rpc.NewHandler("avax", &atomicvm.AvaxAPI{
		Context:      vm.chainContext,
		Mempool:      vm.atomicMempool,
		PushGossiper: vm.pushGossiper,
		AcceptedTxs:  vm.acceptedTxs,
	})
	if err != nil {
		return nil, fmt.Errorf("making AVAX handler: %w", err)
	}
	vm.chainContext.Log.Info("AVAX API enabled")
	apis[avaxEndpoint] = avaxAPI
	return apis, nil
}

// TODO: Correctly block until either the atomic mempool or the evm mempool has
// txs.
func (*VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(time.Second):
		return common.PendingTxs, nil
	}
}

func (vm *VM) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	vm.onShutdown()
	defer vm.wg.Wait()

	return vm.VM.Shutdown(ctx)
}
