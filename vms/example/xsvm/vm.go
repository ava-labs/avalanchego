// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/grpcreflect"
	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/connectproto/pb/xsvm/xsvmconnect"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/builder"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/chain"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/execute"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/mempool"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/state"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"

	smblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	xsblock "github.com/ava-labs/avalanchego/vms/example/xsvm/block"
)

var (
	_ smblock.ChainVM                      = (*VM)(nil)
	_ smblock.BuildBlockWithContextChainVM = (*VM)(nil)

	// mempool gossip parameters
	TargetGossipSize               = 20 * units.KiB
	PushGossipPercentStake         = .9
	PushGossipNumValidators        = 100
	PushGossipNumPeers             = 0
	PushRegossipNumValidators      = 10
	PushRegossipNumPeers           = 0
	PushGossipDiscardedCacheSize   = 16384
	PushGossipMaxRegossipFrequency = 30 * time.Second
	PushGossipFrequency            = 500 * time.Millisecond
	PullGossipPollSize             = 1
	PullGossipFrequency            = 1500 * time.Millisecond
)

type VM struct {
	*p2p.Network

	chainContext *snow.Context
	db           database.Database
	genesis      *genesis.Genesis

	chain   chain.Chain
	mempool *mempool.GossipMempool

	// Cancelled on shutdown
	onShutdownCtx       context.Context
	onShutdownCtxCancel context.CancelFunc

	validators *p2p.Validators
}

func (vm *VM) Initialize(
	_ context.Context,
	chainContext *snow.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	_ []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	chainContext.Log.Info("initializing xsvm",
		zap.Stringer("version", Version),
	)

	metrics := prometheus.NewRegistry()
	err := chainContext.Metrics.Register("p2p", metrics)
	if err != nil {
		return err
	}

	vm.Network, err = p2p.NewNetwork(
		chainContext.Log,
		appSender,
		metrics,
		"",
	)
	if err != nil {
		return err
	}

	// Allow signing of all warp messages. This is not typically safe, but is
	// allowed for this example.
	acp118Handler := acp118.NewHandler(
		acp118Verifier{},
		chainContext.WarpSigner,
	)
	if err := vm.Network.AddHandler(p2p.SignatureRequestHandlerID, acp118Handler); err != nil {
		return err
	}

	vm.chainContext = chainContext
	vm.db = db
	g, err := genesis.Parse(genesisBytes)
	if err != nil {
		return fmt.Errorf("failed to parse genesis bytes: %w", err)
	}

	vdb := versiondb.New(vm.db)
	if err := execute.Genesis(vdb, chainContext.ChainID, g); err != nil {
		return fmt.Errorf("failed to initialize genesis state: %w", err)
	}
	if err := vdb.Commit(); err != nil {
		return err
	}

	vm.genesis = g

	vm.chain, err = chain.New(chainContext, vm.db)
	if err != nil {
		return fmt.Errorf("failed to initialize chain manager: %w", err)
	}

	vm.validators = p2p.NewValidators(
		vm.chainContext.Log,
		vm.chainContext.SubnetID,
		vm.chainContext.ValidatorState,
		time.Minute,
	)

	err = vm.setAndInitializeMempool(metrics)
	if err != nil {
		return fmt.Errorf("failed to initialize mempool: %w", err)
	}

	chainContext.Log.Info("initialized xsvm",
		zap.Stringer("lastAcceptedID", vm.chain.LastAccepted()),
	)
	return nil
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	vm.chain.SetChainState(state)
	return nil
}

func (vm *VM) Shutdown(context.Context) error {
	if vm.chainContext == nil {
		return nil
	}
	vm.chainContext.Log.Info("shutting down xsvm")
	vm.onShutdownCtxCancel()
	return vm.db.Close()
}

func (*VM) Version(context.Context) (string, error) {
	return Version.String(), nil
}

func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	jsonRPCAPI := api.NewServer(
		vm.chainContext,
		vm.genesis,
		vm.db,
		vm.chain,
		vm.mempool,
	)
	return map[string]http.Handler{
		"": server,
	}, server.RegisterService(jsonRPCAPI, constants.XSVMName)
}

func (vm *VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	mux := http.NewServeMux()

	reflectionPattern, reflectionHandler := grpcreflect.NewHandlerV1(
		grpcreflect.NewStaticReflector(xsvmconnect.PingName),
	)
	mux.Handle(reflectionPattern, reflectionHandler)

	pingService := &api.PingService{Log: vm.chainContext.Log}
	pingPath, pingHandler := xsvmconnect.NewPingHandler(pingService)
	mux.Handle(pingPath, pingHandler)

	return mux, nil
}

func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return http.StatusOK, nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.chain.GetBlock(blkID)
}

func (vm *VM) ParseBlock(_ context.Context, blkBytes []byte) (snowman.Block, error) {
	blk, err := xsblock.Parse(blkBytes)
	if err != nil {
		return nil, err
	}
	return vm.chain.NewBlock(blk)
}

func (vm *VM) WaitForEvent(ctx context.Context) (common.Message, error) {
	return vm.mempool.WaitForEvent(ctx)
}

func (vm *VM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return vm.mempool.BuildBlock(ctx, nil)
}

func (vm *VM) SetPreference(_ context.Context, preferred ids.ID) error {
	vm.mempool.SetPreference(preferred)
	return nil
}

func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.chain.LastAccepted(), nil
}

func (vm *VM) BuildBlockWithContext(ctx context.Context, blockContext *smblock.Context) (snowman.Block, error) {
	return vm.mempool.BuildBlock(ctx, blockContext)
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return state.GetBlockIDByHeight(vm.db, height)
}

var _ gossip.Marshaller[*tx.Tx] = (*txMarshaller)(nil)

type txMarshaller struct{}

func (txMarshaller) MarshalGossip(marshalTx *tx.Tx) ([]byte, error) {
	return tx.Codec.Marshal(tx.CodecVersion, marshalTx)
}

func (txMarshaller) UnmarshalGossip(bytes []byte) (*tx.Tx, error) {
	return tx.Parse(bytes)
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, v *version.Application) error {
	vm.validators.Connected(nodeID)
	return vm.Network.Connected(ctx, nodeID, v)
}

// setAndInitializeMempool sets up the mempool along with its associated
// push/pull gossipers. It expects the vm to be initialized with
// chainContext, chain, Network, and validators.
func (vm *VM) setAndInitializeMempool(
	register prometheus.Registerer,
) error {
	mem, err := mempool.NewMempool(
		constants.XSVMName,
		register,
	)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	builder := builder.New(vm.chainContext, vm.chain, mem)
	txGossipClient := vm.Network.NewClient(p2p.TxGossipHandlerID, vm.validators)
	marshaller := &txMarshaller{}

	txGossipMetrics, err := gossip.NewMetrics(register, "tx")
	if err != nil {
		return err
	}
	gossipMempool, err := mempool.NewGossipMempool(
		mem,
		register,
		builder,
	)
	if err != nil {
		return fmt.Errorf("failed to create gossip mempool: %w", err)
	}

	txPushGossiper, err := gossip.NewPushGossiper(
		marshaller,
		gossipMempool,
		vm.validators,
		txGossipClient,
		txGossipMetrics,
		// config parameters from /avm/network/config.go
		gossip.BranchingFactor{
			StakePercentage: PushGossipPercentStake,
			Validators:      PushGossipNumValidators,
			Peers:           PushGossipNumPeers,
		},
		gossip.BranchingFactor{
			Validators: PushRegossipNumValidators,
			Peers:      PushRegossipNumPeers,
		},
		PushGossipDiscardedCacheSize,
		TargetGossipSize,
		PushGossipMaxRegossipFrequency,
	)
	if err != nil {
		return fmt.Errorf("failed to create push gossiper: %w", err)
	}

	var txPullGossiper gossip.Gossiper = gossip.NewPullGossiper[*tx.Tx](
		vm.chainContext.Log,
		marshaller,
		gossipMempool,
		txGossipClient,
		txGossipMetrics,
		PullGossipPollSize,
	)

	handler := gossip.NewHandler[*tx.Tx](
		vm.chainContext.Log,
		marshaller,
		gossipMempool,
		txGossipMetrics,
		TargetGossipSize,
	)

	err = vm.Network.AddHandler(p2p.TxGossipHandlerID, handler)
	if err != nil {
		return fmt.Errorf("failed to add tx gossip handler to network: %w", err)
	}

	// Context to shut down the gossipers
	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())

	// Start the push/pull gossipers
	// TODO: when i passed in chainContext.Log I couldn't see the logs printed during gossiping
	// is there another logger to use?
	go gossip.Every(vm.onShutdownCtx, vm.chainContext.Log, txPushGossiper, PushGossipFrequency)

	go gossip.Every(vm.onShutdownCtx, vm.chainContext.Log, txPullGossiper, PullGossipFrequency)

	vm.mempool = gossipMempool
	return nil
}
