// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/rpc/v2"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	droppedTxCacheSize     = 64
	validatorSetsCacheSize = 64

	// MaxValidatorWeightFactor is the maximum factor of the validator stake
	// that is allowed to be placed on a validator.
	MaxValidatorWeightFactor uint64 = 5

	// Maximum future start time for staking/delegating
	maxFutureStartTime = 24 * 7 * 2 * time.Hour
)

var (
	errInvalidID         = errors.New("invalid ID")
	errDSCantValidate    = errors.New("new blockchain can't be validated by primary network")
	errStartTimeTooEarly = errors.New("start time is before the current chain time")
	errStartAfterEndTime = errors.New("start time is after the end time")
	errWrongCacheType    = errors.New("unexpectedly cached type")

	_ block.ChainVM        = &VM{}
	_ validators.Connector = &VM{}
	_ secp256k1fx.VM       = &VM{}
	_ Fx                   = &secp256k1fx.Fx{}
)

// VM implements the snowman.ChainVM interface
type VM struct {
	Factory
	metrics
	avax.AddressManager
	avax.AtomicUTXOManager
	*network

	// Used to get time. Useful for faking time during tests.
	clock mockable.Clock

	// Used to create and use keys.
	factory crypto.FactorySECP256K1R

	blockBuilder blockBuilder

	uptimeManager uptime.Manager

	rewards reward.Calculator

	// The context of this vm
	ctx       *snow.Context
	dbManager manager.Manager

	// channel to send messages to the consensus engine
	toEngine chan<- common.Message

	internalState InternalState

	// ID of the preferred block
	preferred ids.ID

	// ID of the last accepted block
	lastAcceptedID ids.ID

	fx            Fx
	codecRegistry codec.Registry

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped utils.AtomicBool

	// Contains the IDs of transactions recently dropped because they failed
	// verification. These txs may be re-issued and put into accepted blocks, so
	// check the database to see if it was later committed/aborted before
	// reporting that it's dropped.
	// Key: Tx ID
	// Value: String repr. of the verification error
	droppedTxCache cache.LRU

	// Maps caches for each subnet that is currently whitelisted.
	// Key: Subnet ID
	// Value: cache mapping height -> validator set map
	validatorSetCaches map[ids.ID]cache.Cacher

	// Key: block ID
	// Value: the block
	currentBlocks map[ids.ID]Block
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgs chan<- common.Message,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	ctx.Log.Verbo("initializing platform chain")

	registerer := prometheus.NewRegistry()
	if err := ctx.Metrics.Register(registerer); err != nil {
		return err
	}

	// Initialize metrics as soon as possible
	if err := vm.metrics.Initialize("", registerer); err != nil {
		return err
	}

	// Initialize the utility to parse addresses
	vm.AddressManager = avax.NewAddressManager(ctx)

	// Initialize the utility to fetch atomic UTXOs
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(ctx.SharedMemory, Codec)

	vm.fx = &secp256k1fx.Fx{}

	vm.ctx = ctx
	vm.dbManager = dbManager
	vm.toEngine = msgs

	vm.codecRegistry = linearcodec.NewDefault()
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	vm.droppedTxCache = cache.LRU{Size: droppedTxCacheSize}
	vm.validatorSetCaches = make(map[ids.ID]cache.Cacher)
	vm.currentBlocks = make(map[ids.ID]Block)

	if err := vm.blockBuilder.Initialize(vm, registerer); err != nil {
		return fmt.Errorf(
			"failed to initialize the block builder: %w",
			err,
		)
	}
	vm.network = newNetwork(vm.ApricotPhase4Time, appSender, vm)
	vm.rewards = reward.NewCalculator(vm.RewardConfig)

	is, err := NewMeteredInternalState(vm, vm.dbManager.Current().Database, genesisBytes, registerer)
	if err != nil {
		return err
	}
	vm.internalState = is

	// Initialize the utility to track validator uptimes
	vm.uptimeManager = uptime.NewManager(is)
	vm.UptimeLockedCalculator.SetCalculator(&vm.bootstrapped, &ctx.Lock, vm.uptimeManager)

	if err := vm.updateValidators(); err != nil {
		return fmt.Errorf(
			"failed to initialize validator sets: %w",
			err,
		)
	}

	// Create all of the chains that the database says exist
	if err := vm.initBlockchains(); err != nil {
		return fmt.Errorf(
			"failed to initialize blockchains: %w",
			err,
		)
	}

	vm.lastAcceptedID = is.GetLastAccepted()

	ctx.Log.Info("initializing last accepted block as %s", vm.lastAcceptedID)

	// Build off the most recently accepted block
	return vm.SetPreference(vm.lastAcceptedID)
}

// Create all chains that exist that this node validates.
func (vm *VM) initBlockchains() error {
	if err := vm.createSubnet(constants.PrimaryNetworkID); err != nil {
		return err
	}

	if vm.StakingEnabled {
		for subnetID := range vm.WhitelistedSubnets {
			if err := vm.createSubnet(subnetID); err != nil {
				return err
			}
		}
	} else {
		subnets, err := vm.internalState.GetSubnets()
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
	chains, err := vm.internalState.GetChains(subnetID)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		if err := vm.createChain(chain); err != nil {
			return err
		}
	}
	return nil
}

// Create the blockchain described in [tx], but only if this node is a member of
// the subnet that validates the chain
func (vm *VM) createChain(tx *Tx) error {
	unsignedTx, ok := tx.UnsignedTx.(*UnsignedCreateChainTx)
	if !ok {
		return errWrongTxType
	}

	if vm.StakingEnabled && // Staking is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != unsignedTx.SubnetID && // All nodes must validate the primary network
		!vm.WhitelistedSubnets.Contains(unsignedTx.SubnetID) { // This node doesn't validate this blockchain
		return nil
	}

	chainParams := chains.ChainParameters{
		ID:          tx.ID(),
		SubnetID:    unsignedTx.SubnetID,
		GenesisData: unsignedTx.GenesisData,
		VMAlias:     unsignedTx.VMID.String(),
	}
	for _, fxID := range unsignedTx.FxIDs {
		chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
	}
	vm.Chains.CreateChain(chainParams)
	return nil
}

// onBootstrapStarted marks this VM as bootstrapping
func (vm *VM) onBootstrapStarted() error {
	vm.bootstrapped.SetValue(false)
	return vm.fx.Bootstrapping()
}

// onNormalOperationsStarted marks this VM as bootstrapped
func (vm *VM) onNormalOperationsStarted() error {
	if vm.bootstrapped.GetValue() {
		return nil
	}
	vm.bootstrapped.SetValue(true)

	if err := vm.fx.Bootstrapped(); err != nil {
		return err
	}

	primaryValidatorSet, exist := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	if !exist {
		return errNoPrimaryValidators
	}
	primaryValidators := primaryValidatorSet.List()

	validatorIDs := make([]ids.ShortID, len(primaryValidators))
	for i, vdr := range primaryValidators {
		validatorIDs[i] = vdr.ID()
	}

	if err := vm.uptimeManager.StartTracking(validatorIDs); err != nil {
		return err
	}
	return vm.internalState.Commit()
}

func (vm *VM) SetState(state snow.State) error {
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
func (vm *VM) Shutdown() error {
	if vm.dbManager == nil {
		return nil
	}

	vm.blockBuilder.Shutdown()

	if vm.bootstrapped.GetValue() {
		primaryValidatorSet, exist := vm.Validators.GetValidators(constants.PrimaryNetworkID)
		if !exist {
			return errNoPrimaryValidators
		}
		primaryValidators := primaryValidatorSet.List()

		validatorIDs := make([]ids.ShortID, len(primaryValidators))
		for i, vdr := range primaryValidators {
			validatorIDs[i] = vdr.ID()
		}

		if err := vm.uptimeManager.Shutdown(validatorIDs); err != nil {
			return err
		}
		if err := vm.internalState.Commit(); err != nil {
			return err
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		vm.internalState.Close(),
		vm.dbManager.Close(),
	)
	return errs.Err
}

// BuildBlock builds a block to be added to consensus
func (vm *VM) BuildBlock() (snowman.Block, error) { return vm.blockBuilder.BuildBlock() }

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	var blk Block
	if _, err := Codec.Unmarshal(b, &blk); err != nil {
		return nil, err
	}
	if err := blk.initialize(vm, b, choices.Processing, blk); err != nil {
		return nil, err
	}

	// TODO: remove this to make ParseBlock stateless
	if block, err := vm.GetBlock(blk.ID()); err == nil {
		// If we have seen this block before, return it with the most up-to-date
		// info
		return block, nil
	}
	return blk, nil
}

// GetBlock implements the snowman.ChainVM interface
func (vm *VM) GetBlock(blkID ids.ID) (snowman.Block, error) { return vm.getBlock(blkID) }

func (vm *VM) getBlock(blkID ids.ID) (Block, error) {
	// If block is in memory, return it.
	if blk, exists := vm.currentBlocks[blkID]; exists {
		return blk, nil
	}
	return vm.internalState.GetBlock(blkID)
}

// LastAccepted returns the block most recently accepted
func (vm *VM) LastAccepted() (ids.ID, error) {
	return vm.lastAcceptedID, nil
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(blkID ids.ID) error {
	if blkID == vm.preferred {
		// If the preference didn't change, then this is a noop
		return nil
	}
	vm.preferred = blkID
	vm.blockBuilder.ResetTimer()
	return nil
}

func (vm *VM) Preferred() (Block, error) {
	return vm.getBlock(vm.preferred)
}

// NotifyBlockReady tells the consensus engine that a new block is ready to be
// created
func (vm *VM) NotifyBlockReady() {
	select {
	case vm.toEngine <- common.PendingTxs:
	default:
		vm.ctx.Log.Debug("dropping message to consensus engine")
	}
}

func (vm *VM) Version() (string, error) {
	return version.Current.String(), nil
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
func (vm *VM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	server.RegisterInterceptFunc(vm.metrics.apiRequestMetrics.InterceptRequest)
	server.RegisterAfterFunc(vm.metrics.apiRequestMetrics.AfterRequest)
	if err := server.RegisterService(&Service{vm: vm}, "platform"); err != nil {
		return nil, err
	}

	return map[string]*common.HTTPHandler{
		"": {
			Handler: server,
		},
	}, nil
}

// CreateStaticHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
func (vm *VM) CreateStaticHandlers() (map[string]*common.HTTPHandler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(&StaticService{}, "platform"); err != nil {
		return nil, err
	}

	return map[string]*common.HTTPHandler{
		"": {
			LockOptions: common.NoLock,
			Handler:     server,
		},
	}, nil
}

// Connected implements validators.Connector
func (vm *VM) Connected(vdrID ids.ShortID, nodeVersion version.Application) error {
	return vm.uptimeManager.Connect(vdrID)
}

// Disconnected implements validators.Connector
func (vm *VM) Disconnected(vdrID ids.ShortID) error {
	if err := vm.uptimeManager.Disconnect(vdrID); err != nil {
		return err
	}
	return vm.internalState.Commit()
}

// GetValidatorSet returns the validator set at the specified height for the
// provided subnetID.
func (vm *VM) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
	validatorSetsCache, exists := vm.validatorSetCaches[subnetID]
	if !exists {
		validatorSetsCache = &cache.LRU{Size: validatorSetsCacheSize}
		// Only cache whitelisted subnets
		if vm.WhitelistedSubnets.Contains(subnetID) || subnetID == constants.PrimaryNetworkID {
			vm.validatorSetCaches[subnetID] = validatorSetsCache
		}
	}

	if validatorSetIntf, ok := validatorSetsCache.Get(height); ok {
		validatorSet, ok := validatorSetIntf.(map[ids.ShortID]uint64)
		if !ok {
			return nil, errWrongCacheType
		}
		vm.metrics.validatorSetsCached.Inc()
		return validatorSet, nil
	}

	lastAcceptedHeight, err := vm.GetCurrentHeight()
	if err != nil {
		return nil, err
	}
	if lastAcceptedHeight < height {
		return nil, database.ErrNotFound
	}

	// get the start time to track metrics
	startTime := vm.Clock().Time()

	currentValidators, ok := vm.Validators.GetValidators(subnetID)
	if !ok {
		return nil, errNotEnoughValidators
	}
	currentValidatorList := currentValidators.List()

	vdrSet := make(map[ids.ShortID]uint64, len(currentValidatorList))
	for _, vdr := range currentValidatorList {
		vdrSet[vdr.ID()] = vdr.Weight()
	}

	for i := lastAcceptedHeight; i > height; i-- {
		diffs, err := vm.internalState.GetValidatorWeightDiffs(i, subnetID)
		if err != nil {
			return nil, err
		}

		for nodeID, diff := range diffs {
			var op func(uint64, uint64) (uint64, error)
			if diff.Decrease {
				// The validator's weight was decreased at this block, so in the
				// prior block it was higher.
				op = safemath.Add64
			} else {
				// The validator's weight was increased at this block, so in the
				// prior block it was lower.
				op = safemath.Sub64
			}

			newWeight, err := op(vdrSet[nodeID], diff.Amount)
			if err != nil {
				return nil, err
			}
			if newWeight == 0 {
				delete(vdrSet, nodeID)
			} else {
				vdrSet[nodeID] = newWeight
			}
		}
	}

	// cache the validator set
	validatorSetsCache.Put(height, vdrSet)

	endTime := vm.Clock().Time()
	vm.metrics.validatorSetsCreated.Inc()
	vm.metrics.validatorSetsDuration.Add(float64(endTime.Sub(startTime)))
	vm.metrics.validatorSetsHeightDiff.Add(float64(lastAcceptedHeight - height))
	return vdrSet, nil
}

// GetCurrentHeight returns the height of the last accepted block
func (vm *VM) GetCurrentHeight() (uint64, error) {
	lastAccepted, err := vm.getBlock(vm.lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return lastAccepted.Height(), nil
}

func (vm *VM) updateValidators() error {
	currentValidators := vm.internalState.CurrentStakerChainState()
	primaryValidators, err := currentValidators.ValidatorSet(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}
	if err := vm.Validators.Set(constants.PrimaryNetworkID, primaryValidators); err != nil {
		return err
	}

	weight, _ := primaryValidators.GetWeight(vm.ctx.NodeID)
	vm.localStake.Set(float64(weight))
	vm.totalStake.Set(float64(primaryValidators.Weight()))

	for subnetID := range vm.WhitelistedSubnets {
		subnetValidators, err := currentValidators.ValidatorSet(subnetID)
		if err != nil {
			return err
		}
		if err := vm.Validators.Set(subnetID, subnetValidators); err != nil {
			return err
		}
	}
	return nil
}

// Returns the time when the next staker of any subnet starts/stops staking
// after the current timestamp
func (vm *VM) nextStakerChangeTime(vs ValidatorState) (time.Time, error) {
	currentStakers := vs.CurrentStakerChainState()
	pendingStakers := vs.PendingStakerChainState()

	earliest := mockable.MaxTime
	if currentStakers := currentStakers.Stakers(); len(currentStakers) > 0 {
		nextStakerToRemove := currentStakers[0]
		staker, ok := nextStakerToRemove.UnsignedTx.(TimedTx)
		if !ok {
			return time.Time{}, errWrongTxType
		}
		endTime := staker.EndTime()
		if endTime.Before(earliest) {
			earliest = endTime
		}
	}
	if pendingStakers := pendingStakers.Stakers(); len(pendingStakers) > 0 {
		nextStakerToAdd := pendingStakers[0]
		staker, ok := nextStakerToAdd.UnsignedTx.(TimedTx)
		if !ok {
			return time.Time{}, errWrongTxType
		}
		startTime := staker.StartTime()
		if startTime.Before(earliest) {
			earliest = startTime
		}
	}
	return earliest, nil
}

func (vm *VM) CodecRegistry() codec.Registry { return vm.codecRegistry }

func (vm *VM) Clock() *mockable.Clock { return &vm.clock }

func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

// Returns the percentage of the total stake on the Primary Network of nodes
// connected to this node.
func (vm *VM) getPercentConnected() (float64, error) {
	vdrSet, exists := vm.Validators.GetValidators(constants.PrimaryNetworkID)
	if !exists {
		return 0, errNoPrimaryValidators
	}

	vdrs := vdrSet.List()

	var (
		connectedStake uint64
		err            error
	)
	for _, vdr := range vdrs {
		if !vm.uptimeManager.IsConnected(vdr.ID()) {
			continue // not connected to us --> don't include
		}
		connectedStake, err = safemath.Add64(connectedStake, vdr.Weight())
		if err != nil {
			return 0, err
		}
	}
	return float64(connectedStake) / float64(vdrSet.Weight()), nil
}
