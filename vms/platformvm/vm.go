// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/rpc/v2"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/network"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	snowmanblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	blockbuilder "github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	pvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var (
	_ snowmanblock.ChainVM       = (*VM)(nil)
	_ secp256k1fx.VM             = (*VM)(nil)
	_ validators.State           = (*VM)(nil)
	_ validators.SubnetConnector = (*VM)(nil)
)

type VM struct {
	config.Config
	blockbuilder.Builder
	network.Network
	validators.State

	metrics            metrics.Metrics
	atomicUtxosManager avax.AtomicUTXOManager

	// Used to get time. Useful for faking time during tests.
	clock mockable.Clock

	uptimeManager uptime.Manager

	// The context of this vm
	ctx *snow.Context
	db  database.Database

	state state.State

	fx            fx.Fx
	codecRegistry codec.Registry

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped utils.Atomic[bool]

	txBuilder txbuilder.Builder
	manager   blockexecutor.Manager

	// TODO: Remove after v1.11.x is activated
	pruned utils.Atomic[bool]
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	_ []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	chainCtx.Log.Verbo("initializing platform chain")

	execConfig, err := config.GetExecutionConfig(configBytes)
	if err != nil {
		return err
	}
	chainCtx.Log.Info("using VM execution config", zap.Reflect("config", execConfig))

	registerer := prometheus.NewRegistry()
	if err := chainCtx.Metrics.Register(registerer); err != nil {
		return err
	}

	// Initialize metrics as soon as possible
	vm.metrics, err = metrics.New("", registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.ctx = chainCtx
	vm.db = db

	vm.codecRegistry = linearcodec.NewDefault()
	vm.fx = &secp256k1fx.Fx{}
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	rewards := reward.NewCalculator(vm.RewardConfig)

	vm.state, err = state.New(
		vm.db,
		genesisBytes,
		registerer,
		&vm.Config,
		execConfig,
		vm.ctx,
		vm.metrics,
		rewards,
	)
	if err != nil {
		return err
	}

	validatorManager := pvalidators.NewManager(chainCtx.Log, vm.Config, vm.state, vm.metrics, &vm.clock)
	vm.State = validatorManager
	vm.atomicUtxosManager = avax.NewAtomicUTXOManager(chainCtx.SharedMemory, txs.Codec)
	utxoHandler := utxo.NewHandler(vm.ctx, &vm.clock, vm.fx)
	vm.uptimeManager = uptime.NewManager(vm.state, &vm.clock)
	vm.UptimeLockedCalculator.SetCalculator(&vm.bootstrapped, &chainCtx.Lock, vm.uptimeManager)

	vm.txBuilder = txbuilder.New(
		vm.ctx,
		&vm.Config,
		&vm.clock,
		vm.fx,
		vm.state,
		vm.atomicUtxosManager,
		utxoHandler,
	)

	txExecutorBackend := &txexecutor.Backend{
		Config:       &vm.Config,
		Ctx:          vm.ctx,
		Clk:          &vm.clock,
		Fx:           vm.fx,
		FlowChecker:  utxoHandler,
		Uptimes:      vm.uptimeManager,
		Rewards:      rewards,
		Bootstrapped: &vm.bootstrapped,
	}

	// Note: There is a circular dependency between the mempool and block
	//       builder which is broken by passing in the vm.
	mempool, err := mempool.New("mempool", registerer, vm)
	if err != nil {
		return fmt.Errorf("failed to create mempool: %w", err)
	}

	vm.manager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		txExecutorBackend,
		validatorManager,
	)
	vm.Network = network.New(
		txExecutorBackend.Ctx,
		vm.manager,
		mempool,
		txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		appSender,
	)
	vm.Builder = blockbuilder.New(
		mempool,
		vm.txBuilder,
		txExecutorBackend,
		vm.manager,
		toEngine,
	)

	// Create all of the chains that the database says exist
	if err := vm.initBlockchains(); err != nil {
		return fmt.Errorf(
			"failed to initialize blockchains: %w",
			err,
		)
	}

	lastAcceptedID := vm.state.GetLastAccepted()
	chainCtx.Log.Info("initializing last accepted",
		zap.Stringer("blkID", lastAcceptedID),
	)
	if err := vm.SetPreference(ctx, lastAcceptedID); err != nil {
		return err
	}

	shouldPrune, err := vm.state.ShouldPrune()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database should be pruned: %w",
			err,
		)
	}
	if !shouldPrune {
		chainCtx.Log.Info("state already pruned and indexed")
		vm.pruned.Set(true)
		return nil
	}

	go func() {
		err := vm.state.PruneAndIndex(&vm.ctx.Lock, vm.ctx.Log)
		if err != nil {
			vm.ctx.Log.Error("state pruning and height indexing failed",
				zap.Error(err),
			)
		}

		vm.pruned.Set(true)
	}()

	return nil
}

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	if vm.Config.PartialSyncPrimaryNetwork {
		vm.ctx.Log.Info("skipping primary network chain creation")
	} else if err := vm.createSubnet(constants.PrimaryNetworkID); err != nil {
		return err
	}

	if vm.SybilProtectionEnabled {
		for subnetID := range vm.TrackedSubnets {
			if err := vm.createSubnet(subnetID); err != nil {
				return err
			}
		}
	} else {
		subnets, err := vm.state.GetSubnets()
		if err != nil {
			return err
		}
		for _, subnet := range subnets {
			if err := vm.createSubnet(subnet.ID()); err != nil {
				return err
			}
		}
	}
	return nil
}

// Create the subnet with ID [subnetID]
func (vm *VM) createSubnet(subnetID ids.ID) error {
	chains, err := vm.state.GetChains(subnetID)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		tx, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}
		vm.Config.CreateChain(chain.ID(), tx)
	}
	return nil
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.Set(false)
	return vm.fx.Bootstrapping()
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.Get() {
		return nil
	}
	vm.bootstrapped.Set(true)

	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}

	primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
	if err := vm.uptimeManager.StartTracking(primaryVdrIDs, constants.PrimaryNetworkID); err != nil {
		return err
	}

	vl := validators.NewLogger(vm.ctx.Log, constants.PrimaryNetworkID, vm.ctx.NodeID)
	vm.Validators.RegisterCallbackListener(constants.PrimaryNetworkID, vl)

	for subnetID := range vm.TrackedSubnets {
		vdrIDs := vm.Validators.GetValidatorIDs(subnetID)
		if err := vm.uptimeManager.StartTracking(vdrIDs, subnetID); err != nil {
			return err
		}

		vl := validators.NewLogger(vm.ctx.Log, subnetID, vm.ctx.NodeID)
		vm.Validators.RegisterCallbackListener(subnetID, vl)
	}

	if err := vm.state.Commit(); err != nil {
		return err
	}

	// Start the block builder
	vm.Builder.ResetBlockTimer()
	return nil
}

func (vm *VM) SetState(_ context.Context, state snow.State) error {
	switch state {
	case snow.Bootstrapping:
		return vm.onBootstrapStarted()
	case snow.NormalOp:
		return vm.onNormalOperationsStarted()
	default:
		return snow.ErrUnknownState
	}
}

// Shutdown this blockchain
func (vm *VM) Shutdown(context.Context) error {
	if vm.db == nil {
		return nil
	}

	vm.Builder.Shutdown()

	if vm.bootstrapped.Get() {
		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
		if err := vm.uptimeManager.StopTracking(primaryVdrIDs, constants.PrimaryNetworkID); err != nil {
			return err
		}

		for subnetID := range vm.TrackedSubnets {
			vdrIDs := vm.Validators.GetValidatorIDs(subnetID)
			if err := vm.uptimeManager.StopTracking(vdrIDs, subnetID); err != nil {
				return err
			}
		}

		if err := vm.state.Commit(); err != nil {
			return err
		}
	}

	return utils.Err(
		vm.state.Close(),
		vm.db.Close(),
	)
}

func (vm *VM) ParseBlock(_ context.Context, b []byte) (snowman.Block, error) {
	// Note: blocks to be parsed are not verified, so we must used blocks.Codec
	// rather than blocks.GenesisCodec
	statelessBlk, err := block.Parse(block.Codec, b)
	if err != nil {
		return nil, err
	}
	return vm.manager.NewBlock(statelessBlk), nil
}

func (vm *VM) GetBlock(_ context.Context, blkID ids.ID) (snowman.Block, error) {
	return vm.manager.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted(context.Context) (ids.ID, error) {
	return vm.manager.LastAccepted(), nil
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(_ context.Context, blkID ids.ID) error {
	if vm.manager.SetPreference(blkID) {
		vm.Builder.ResetBlockTimer()
	}
	return nil
}

func (*VM) Version(context.Context) (string, error) {
	return version.Current.String(), nil
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
func (vm *VM) CreateHandlers(context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterInterceptFunc(vm.metrics.InterceptRequest)
	server.RegisterAfterFunc(vm.metrics.AfterRequest)
	service := &Service{
		vm:          vm,
		addrManager: avax.NewAddressManager(vm.ctx),
		stakerAttributesCache: &cache.LRU[ids.ID, *stakerAttributes]{
			Size: stakerAttributesCacheSize,
		},
	}
	err := server.RegisterService(service, "platform")
	return map[string]http.Handler{
		"": server,
	}, err
}

// CreateStaticHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
func (*VM) CreateStaticHandlers(context.Context) (map[string]http.Handler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	return map[string]http.Handler{
		"": server,
	}, server.RegisterService(&api.StaticService{}, "platform")
}

func (vm *VM) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	return vm.uptimeManager.Connect(nodeID, constants.PrimaryNetworkID)
}

func (vm *VM) ConnectedSubnet(_ context.Context, nodeID ids.NodeID, subnetID ids.ID) error {
	return vm.uptimeManager.Connect(nodeID, subnetID)
}

func (vm *VM) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	if err := vm.uptimeManager.Disconnect(nodeID); err != nil {
		return err
	}
	return vm.state.Commit()
}

func (vm *VM) CodecRegistry() codec.Registry {
	return vm.codecRegistry
}

func (vm *VM) Clock() *mockable.Clock {
	return &vm.clock
}

func (vm *VM) Logger() logging.Logger {
	return vm.ctx.Log
}

func (vm *VM) VerifyHeightIndex(_ context.Context) error {
	if vm.pruned.Get() {
		return nil
	}

	return snowmanblock.ErrIndexIncomplete
}

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}
