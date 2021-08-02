// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/rpc/v2"

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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/request"
	"github.com/ava-labs/avalanchego/vms/platformvm/uptime"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// PercentDenominator is the denominator used to calculate percentages
	PercentDenominator = 1000000

	droppedTxCacheSize = 50

	maxUTXOsToFetch = 1024

	// TODO: Turn these constants into governable parameters

	// MaxSubMinConsumptionRate is the % consumption that incentivizes staking
	// longer
	MaxSubMinConsumptionRate = 20000 // 2%
	// MinConsumptionRate is the minimum % consumption of the remaining tokens
	// to be minted
	MinConsumptionRate = 100000 // 10%

	// The maximum amount of weight on a validator is required to be no more
	// than [MaxValidatorWeightFactor] * the validator's stake amount.
	MaxValidatorWeightFactor uint64 = 5

	// SupplyCap is the maximum amount of AVAX that should ever exist
	SupplyCap = 720 * units.MegaAvax

	// Maximum future start time for staking/delegating
	maxFutureStartTime = 24 * 7 * 2 * time.Hour
)

var (
	errInvalidID         = errors.New("invalid ID")
	errDSCantValidate    = errors.New("new blockchain can't be validated by primary network")
	errStartTimeTooEarly = errors.New("start time is before the current chain time")
	errStartAfterEndTime = errors.New("start time is after the end time")

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
	uptime.Manager

	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	// Used to create and use keys.
	factory crypto.FactorySECP256K1R

	mempool   Mempool
	appSender common.AppSender
	request.Handler

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
	codec         codec.Manager
	codecRegistry codec.Registry

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped bool

	// Contains the IDs of transactions recently dropped because they failed verification.
	// These txs may be re-issued and put into accepted blocks, so check the database
	// to see if it was later committed/aborted before reporting that it's dropped.
	// Key: Tx ID
	// Value: String repr. of the verification error
	droppedTxCache cache.LRU

	// Key: block ID
	// Value: the block
	currentBlocks map[ids.ID]Block

	lastVdrUpdate time.Time
}

// implements SnowmanPlusPlusVM interface
func (vm *VM) GetActivationTime() time.Time {
	// ProposerVM activation time for snowman++ protocol
	return time.Unix(0, 0)
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

	// Initialize metrics as soon as possible
	if err := vm.metrics.Initialize(ctx.Namespace, ctx.Metrics); err != nil {
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

	vm.codec = Codec
	vm.codecRegistry = linearcodec.NewDefault()

	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	vm.droppedTxCache = cache.LRU{Size: droppedTxCacheSize}
	vm.currentBlocks = make(map[ids.ID]Block)

	vm.mempool.Initialize(vm)
	vm.appSender = appSender
	vm.Handler = request.NewHandler()

	is, err := NewMeteredInternalState(vm, vm.dbManager.Current().Database, genesisBytes, ctx.Namespace, ctx.Metrics)
	if err != nil {
		return err
	}
	vm.internalState = is

	// Initialize the utility to track validator uptimes
	vm.Manager = uptime.NewManager(is)

	if err := vm.updateValidators(true); err != nil {
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
	chains, err := vm.internalState.GetChains(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		if err := vm.createChain(chain); err != nil {
			return err
		}
	}

	for subnetID := range vm.WhitelistedSubnets {
		chains, err := vm.internalState.GetChains(subnetID)
		if err != nil {
			return err
		}
		for _, chain := range chains {
			if err := vm.createChain(chain); err != nil {
				return err
			}
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

// Bootstrapping marks this VM as bootstrapping
func (vm *VM) Bootstrapping() error {
	vm.bootstrapped = false
	return vm.fx.Bootstrapping()
}

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error {
	if vm.bootstrapped {
		return nil
	}
	vm.bootstrapped = true

	errs := wrappers.Errs{}
	errs.Add(
		vm.updateValidators(false),
		vm.fx.Bootstrapped(),
		vm.migrateUptimes(),
	)
	if errs.Errored() {
		return errs.Err
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

	if err := vm.StartTracking(validatorIDs); err != nil {
		return err
	}
	return vm.internalState.Commit()
}

// Shutdown this blockchain
func (vm *VM) Shutdown() error {
	if vm.dbManager == nil {
		return nil
	}

	vm.mempool.Shutdown()

	if vm.bootstrapped {
		primaryValidatorSet, exist := vm.Validators.GetValidators(constants.PrimaryNetworkID)
		if !exist {
			return errNoPrimaryValidators
		}
		primaryValidators := primaryValidatorSet.List()

		validatorIDs := make([]ids.ShortID, len(primaryValidators))
		for i, vdr := range primaryValidators {
			validatorIDs[i] = vdr.ID()
		}

		if err := vm.Manager.Shutdown(validatorIDs); err != nil {
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
func (vm *VM) BuildBlock() (snowman.Block, error) { return vm.mempool.BuildBlock() }

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(b []byte) (snowman.Block, error) {
	var blk Block
	if _, err := GenesisCodec.Unmarshal(b, &blk); err != nil {
		return nil, err
	}
	if err := blk.initialize(vm, b, choices.Processing, blk); err != nil {
		return nil, err
	}

	if block, err := vm.GetBlock(blk.ID()); err == nil {
		// If we have seen this block before, return it with the most up-to-date
		// info
		return block, nil
	}

	vm.internalState.AddBlock(blk)
	return blk, vm.internalState.Commit()
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
	vm.mempool.ResetTimer()
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

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	vm.ctx.Log.Warn("Failed AppRequest to node %v, reqID %v", nodeID, requestID)
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppRequest(nodeID ids.ShortID, requestID uint32, request []byte) error {
	vm.ctx.Log.Verbo("called AppRequest")

	// decode single id
	txID := ids.ID{}
	_, err := vm.codec.Unmarshal(request, &txID) // TODO: cleanup way we serialize this
	if err != nil {
		vm.ctx.Log.Debug("AppRequest: failed unmarshalling request from Node %v, reqID %v, err %v",
			nodeID, requestID, err)
		return err
	}
	vm.ctx.Log.Debug("called AppRequest with txID %v", txID)

	if !vm.mempool.has(txID) {
		return nil // return nothing is tx is unknown. Is it fine?
	}

	// fetch tx
	resTx, ok := vm.mempool.unissuedTxs[txID]
	if !ok {
		return fmt.Errorf("incoherent mempool. Could not find registered tx")
	}

	// send response
	response, err := vm.codec.Marshal(codecVersion, *resTx)
	if err != nil {
		return err
	}

	vm.appSender.SendAppResponse(nodeID, requestID, response)
	return nil
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	vm.ctx.Log.Verbo("called AppResponse")

	// check requestID
	if err := vm.ReclaimID(requestID); err != nil {
		vm.ctx.Log.Debug("Received an Out-of-Sync AppRequest - nodeID: %v - requestID: %v",
			nodeID, requestID)
		return nil
	}

	// decode single tx
	tx := &Tx{}
	_, err := vm.codec.Unmarshal(response, tx) // TODO: cleanup way we serialize this
	if err != nil {
		vm.ctx.Log.Debug("AppResponse: failed unmarshalling response from Node %v, reqID %v, err %v",
			nodeID, requestID, err)
		return err
	}
	unsignedBytes, err := vm.codec.Marshal(codecVersion, &tx.UnsignedTx)
	if err != nil {
		vm.ctx.Log.Debug("AppResponse: failed unmarshalling unsignedTx from Node %v, reqID %v, err %v",
			nodeID, requestID, err)
		return err
	}
	tx.Initialize(unsignedBytes, response)
	vm.ctx.Log.Debug("called AppResponse with txID %v", tx.ID())

	// validate tx
	switch typedTx := tx.UnsignedTx.(type) {
	case UnsignedDecisionTx:
		if err := typedTx.SynctacticVerify(vm); err != nil {
			vm.ctx.Log.Warn("AppResponse: UnsignedDecisionTx %v is syntactically invalid, err %v. Rejecting it.",
				tx.ID(), err)
			return nil // TODO handle rejection
		}
	case UnsignedProposalTx:
		if err := typedTx.SynctacticVerify(vm); err != nil {
			vm.ctx.Log.Warn("AppResponse: UnsignedProposalTx %v is syntactically invalid, err %v. Rejecting it.",
				tx.ID(), err)
			return err // TODO handle rejection
		}
	case UnsignedAtomicTx:
		if err := typedTx.SynctacticVerify(vm); err != nil {
			vm.ctx.Log.Warn("AppResponse: UnsignedAtomicTx %v is syntactically invalid, err %v. Rejecting it.",
				tx.ID(), err)
			return err // TODO handle rejection
		}
	default:
		return errUnknownTxType
	}

	// add to mempool and possibly re-gossip
	switch err := vm.mempool.AddUncheckedTx(tx); err {
	case nil:
		txID := tx.ID()
		vm.ctx.Log.Debug("Gossiping txID %v", txID)
		txIDBytes, err := vm.codec.Marshal(codecVersion, txID)
		if err != nil {
			return err
		}
		vm.appSender.SendAppGossip(txIDBytes)
		return nil
	case errTxExceedingMempoolSize:
		// tx has not been accepted to mempool due to size
		// do not gossip since we cannot serve it
		return nil
	case errAttemptReRegisterTx:
		// we already have the tx [maybe requested multiple times to different nodes?].
		// Do not regossip
		return nil
	default:
		// TODO: record among rejected to avoid requesting again
		vm.ctx.Log.Debug("AppResponse: failed AddUnchecked response from Node %v, reqID %v, err %v",
			nodeID, requestID, err)
		return err
	}
}

// This VM doesn't (currently) have any app-specific messages
func (vm *VM) AppGossip(nodeID ids.ShortID, msg []byte) error {
	vm.ctx.Log.Verbo("called AppGossip")

	// decode single id
	txID := ids.ID{}
	_, err := vm.codec.Unmarshal(msg, &txID) // TODO: cleanup way we serialize this
	if err != nil {
		vm.ctx.Log.Debug("AppGossip: failed unmarshalling message from Node %v, err %v", nodeID, err)
		return err
	}
	vm.ctx.Log.Debug("called AppGossip with txID %v", txID)

	if vm.mempool.has(txID) {
		return nil
	}

	nodesSet := ids.NewShortSet(1)
	nodesSet.Add(nodeID)
	vm.appSender.SendAppRequest(nodesSet, vm.IssueID(), msg)

	return nil
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
func (vm *VM) Connected(vdrID ids.ShortID) error {
	return vm.Connect(vdrID)
}

// Disconnected implements validators.Connector
func (vm *VM) Disconnected(vdrID ids.ShortID) error {
	if err := vm.Disconnect(vdrID); err != nil {
		return err
	}
	return vm.internalState.Commit()
}

// GetValidatorSet returns the validator set at the specified height for the
// provided subnetID. Implements validators.VM interface
func (vm *VM) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
	lastAcceptedHeight, err := vm.GetCurrentHeight()
	switch {
	case err != nil:
		return nil, err
	case lastAcceptedHeight < height:
		return nil, database.ErrNotFound
	}

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
	return vdrSet, nil
}

// Implements validators.VM interface
func (vm *VM) GetCurrentHeight() (uint64, error) {
	lastAccepted, err := vm.getBlock(vm.lastAcceptedID)
	if err != nil {
		return 0, err
	}
	return lastAccepted.Height(), nil
}

func (vm *VM) updateValidators(force bool) error {
	now := vm.clock.Time()
	if !force && !vm.bootstrapped && now.Sub(vm.lastVdrUpdate) < 5*time.Second {
		return nil
	}
	vm.lastVdrUpdate = now

	currentValidators := vm.internalState.CurrentStakerChainState()
	primaryValidators, err := currentValidators.ValidatorSet(constants.PrimaryNetworkID)
	if err != nil {
		return err
	}
	if err := vm.Validators.Set(constants.PrimaryNetworkID, primaryValidators); err != nil {
		return err
	}
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

	earliest := timer.MaxTime
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

func (vm *VM) Codec() codec.Manager { return vm.codec }

func (vm *VM) CodecRegistry() codec.Registry { return vm.codecRegistry }

func (vm *VM) Clock() *timer.Clock { return &vm.clock }

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
		if !vm.IsConnected(vdr.ID()) {
			continue // not connected to us --> don't include
		}
		connectedStake, err = safemath.Add64(connectedStake, vdr.Weight())
		if err != nil {
			return 0, err
		}
	}
	return float64(connectedStake) / float64(vdrSet.Weight()), nil
}
