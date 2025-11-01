// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/cache/lru"
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
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/network"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/utxo"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"

	snowmanblock "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	blockbuilder "github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	platformvmmetrics "github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	pmempool "github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	pvalidators "github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var (
	_ snowmanblock.ChainVM                         = (*VM)(nil)
	_ snowmanblock.BuildBlockWithContextChainVM    = (*VM)(nil)
	_ snowmanblock.SetPreferenceWithContextChainVM = (*VM)(nil)
	_ secp256k1fx.VM                               = (*VM)(nil)
	_ validators.State                             = (*VM)(nil)
)

type VM struct {
	config.Internal
	blockbuilder.Builder
	*network.Network
	validators.State
	// TODO remove this once vm lazy initialize is removed
	MempoolFunc func() pmempool.Mempool

	metrics platformvmmetrics.Metrics

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

	manager blockexecutor.Manager

	// Cancelled on shutdown
	onShutdownCtx context.Context
	// Call [onShutdownCtxCancel] to cancel [onShutdownCtx] during Shutdown()
	onShutdownCtxCancel context.CancelFunc
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
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	chainCtx.Log.Verbo("initializing platform chain")

	execConfig, err := config.GetConfig(configBytes)
	if err != nil {
		return err
	}
	chainCtx.Log.Info("using VM execution config", zap.Reflect("config", execConfig))

	registerer, err := metrics.MakeAndRegister(chainCtx.Metrics, "")
	if err != nil {
		return err
	}

	// Initialize metrics as soon as possible
	vm.metrics, err = platformvmmetrics.New(registerer)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	vm.ctx = chainCtx
	vm.db = db

	// Note: this codec is never used to serialize anything
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
		vm.Internal.Validators,
		vm.Internal.UpgradeConfig,
		execConfig,
		vm.ctx,
		vm.metrics,
		rewards,
	)
	if err != nil {
		return err
	}

	validatorManager := pvalidators.NewManager(vm.Internal, vm.state, vm.metrics, &vm.clock)
	vm.State = validatorManager
	utxoVerifier := utxo.NewVerifier(vm.ctx, &vm.clock, vm.fx)
	vm.uptimeManager = uptime.NewManager(vm.state, &vm.clock)
	vm.UptimeLockedCalculator.SetCalculator(&vm.bootstrapped, &chainCtx.Lock, vm.uptimeManager)

	txExecutorBackend := &txexecutor.Backend{
		Config:       &vm.Internal,
		Ctx:          vm.ctx,
		Clk:          &vm.clock,
		Fx:           vm.fx,
		FlowChecker:  utxoVerifier,
		Uptimes:      vm.uptimeManager,
		Rewards:      rewards,
		Bootstrapped: &vm.bootstrapped,
	}

	var mempool pmempool.Mempool
	if vm.MempoolFunc != nil {
		mempool = vm.MempoolFunc()
	} else {
		mempool, err = pmempool.New(
			"mempool",
			vm.Internal.DynamicFeeConfig.Weights,
			execConfig.MempoolGasCapacity,
			vm.ctx.AVAXAssetID,
			registerer,
		)
		if err != nil {
			return fmt.Errorf("failed to create mempool: %w", err)
		}
	}

	vm.manager = blockexecutor.NewManager(
		mempool,
		vm.metrics,
		vm.state,
		txExecutorBackend,
		validatorManager,
	)

	txVerifier := network.NewLockedTxVerifier(&txExecutorBackend.Ctx.Lock, vm.manager)
	vm.Network, err = network.New(
		chainCtx.Log,
		chainCtx.NodeID,
		chainCtx.SubnetID,
		validators.NewLockedState(
			&chainCtx.Lock,
			validatorManager,
		),
		txVerifier,
		mempool,
		txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		appSender,
		chainCtx.Lock.RLocker(),
		vm.state,
		chainCtx.WarpSigner,
		registerer,
		execConfig.Network,
	)
	if err != nil {
		return fmt.Errorf("failed to initialize network: %w", err)
	}

	vm.onShutdownCtx, vm.onShutdownCtxCancel = context.WithCancel(context.Background())
	// TODO: Wait for this goroutine to exit during Shutdown once the platformvm
	// has better control of the context lock.
	go vm.Network.PushGossip(vm.onShutdownCtx)
	go vm.Network.PullGossip(vm.onShutdownCtx)

	vm.Builder = blockbuilder.New(
		mempool,
		txExecutorBackend,
		vm.manager,
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

	// Incrementing [awaitShutdown] would cause a deadlock since
	// [periodicallyPruneMempool] grabs the context lock.
	go vm.periodicallyPruneMempool(execConfig.MempoolPruneFrequency)

	go func() {
		err := vm.state.ReindexBlocks(&vm.ctx.Lock, vm.ctx.Log)
		if err != nil {
			vm.ctx.Log.Warn("reindexing blocks failed",
				zap.Error(err),
			)
		}
	}()

	return nil
}

func (vm *VM) periodicallyPruneMempool(frequency time.Duration) {
	ticker := time.NewTicker(frequency)
	defer ticker.Stop()

	for {
		select {
		case <-vm.onShutdownCtx.Done():
			return
		case <-ticker.C:
			if err := vm.pruneMempool(); err != nil {
				vm.ctx.Log.Debug("pruning mempool failed",
					zap.Error(err),
				)
			}
		}
	}
}

func (vm *VM) pruneMempool() error {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// Packing all of the transactions in order performs additional checks that
	// the MempoolTxVerifier doesn't include. So, evicting transactions from
	// here is expected to happen occasionally.
	blockTxs, err := vm.Builder.PackAllBlockTxs()
	if err != nil {
		return err
	}

	for _, tx := range blockTxs {
		if err := vm.Builder.Add(tx); err != nil {
			vm.ctx.Log.Debug(
				"failed to reissue tx",
				zap.Stringer("txID", tx.ID()),
				zap.Error(err),
			)
		}
	}

	return nil
}

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	if vm.Internal.PartialSyncPrimaryNetwork {
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
		subnetIDs, err := vm.state.GetSubnetIDs()
		if err != nil {
			return err
		}
		for _, subnetID := range subnetIDs {
			if err := vm.createSubnet(subnetID); err != nil {
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
		vm.Internal.CreateChain(chain.ID(), tx)
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

	if !vm.uptimeManager.StartedTracking() {
		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
		if err := vm.uptimeManager.StartTracking(primaryVdrIDs); err != nil {
			return err
		}
	}

	vl := validators.NewLogger(vm.ctx.Log, constants.PrimaryNetworkID, vm.ctx.NodeID)
	vm.Validators.RegisterSetCallbackListener(constants.PrimaryNetworkID, vl)

	for subnetID := range vm.TrackedSubnets {
		vl := validators.NewLogger(vm.ctx.Log, subnetID, vm.ctx.NodeID)
		vm.Validators.RegisterSetCallbackListener(subnetID, vl)
	}

	return vm.state.Commit()
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

	vm.onShutdownCtxCancel()

	if vm.uptimeManager.StartedTracking() {
		primaryVdrIDs := vm.Validators.GetValidatorIDs(constants.PrimaryNetworkID)
		if err := vm.uptimeManager.StopTracking(primaryVdrIDs); err != nil {
			return err
		}

		if err := vm.state.Commit(); err != nil {
			return err
		}
	}

	return errors.Join(
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
	vm.manager.SetPreference(blkID, nil)
	return nil
}

func (vm *VM) SetPreferenceWithContext(_ context.Context, blkID ids.ID, blockCtx *snowmanblock.Context) error {
	vm.manager.SetPreference(blkID, blockCtx)
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
		vm:                    vm,
		addrManager:           avax.NewAddressManager(vm.ctx),
		stakerAttributesCache: lru.NewCache[ids.ID, *stakerAttributes](stakerAttributesCacheSize),
	}
	err := server.RegisterService(service, "platform")
	return map[string]http.Handler{
		"": server,
	}, err
}

func (*VM) NewHTTPHandler(context.Context) (http.Handler, error) {
	return nil, nil
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	if err := vm.uptimeManager.Connect(nodeID); err != nil {
		return err
	}
	return vm.Network.Connected(ctx, nodeID, version)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	if err := vm.uptimeManager.Disconnect(nodeID); err != nil {
		return err
	}
	if err := vm.state.Commit(); err != nil {
		return err
	}
	return vm.Network.Disconnected(ctx, nodeID)
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

func (vm *VM) GetBlockIDAtHeight(_ context.Context, height uint64) (ids.ID, error) {
	return vm.state.GetBlockIDAtHeight(height)
}

func (vm *VM) issueTxFromRPC(tx *txs.Tx) error {
	err := vm.Network.IssueTxFromRPC(tx)
	if err != nil && !errors.Is(err, mempool.ErrDuplicateTx) {
		vm.ctx.Log.Debug("failed to add tx to mempool",
			zap.Stringer("txID", tx.ID()),
			zap.Error(err),
		)
		return err
	}

	return nil
}
