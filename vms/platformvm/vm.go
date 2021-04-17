// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/core"
	"github.com/ava-labs/avalanchego/vms/platformvm/uptime"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// TODO: remove skipped values on the next DB migration
	// For putting/getting values from state
	validatorsTypeID uint64 = iota
	chainsTypeID
	_
	subnetsTypeID
	utxoTypeID
	_
	txTypeID
	statusTypeID
	currentSupplyTypeID

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

	// SupplyCap is the maximum amount of AVAX that should ever exist
	SupplyCap = 720 * units.MegaAvax

	// Maximum future start time for staking/delegating
	maxFutureStartTime = 24 * 7 * 2 * time.Hour
)

var (
	timestampKey     = ids.ID{'t', 'i', 'm', 'e'}
	chainsKey        = ids.ID{'c', 'h', 'a', 'i', 'n', 's'}
	subnetsKey       = ids.ID{'s', 'u', 'b', 'n', 'e', 't', 's'}
	currentSupplyKey = ids.ID{'c', 'u', 'r', 'r', 'e', 't', ' ', 's', 'u', 'p', 'p', 'l', 'y'}
	migratedKey      = []byte("migrated")
	noMigration      = []byte("no migration")

	errRegisteringType          = errors.New("error registering type with database")
	errInvalidLastAcceptedBlock = errors.New("last accepted block must be a decision block")
	errInvalidID                = errors.New("invalid ID")
	errDSCantValidate           = errors.New("new blockchain can't be validated by primary network")
	errStartTimeTooLate         = errors.New("start time is too far in the future")
	errStartTimeTooEarly        = errors.New("start time is before the current chain time")
	errStartAfterEndTime        = errors.New("start time is after the end time")

	_ block.ChainVM        = &VM{}
	_ validators.Connector = &VM{}
)

// VM implements the snowman.ChainVM interface
type VM struct {
	metrics
	avax.AddressManager
	avax.AtomicUTXOManager
	uptime.Manager

	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	// Used to create and use keys.
	factory crypto.FactorySECP256K1R

	// true if the node is being run with staking enabled
	stakingEnabled bool

	// Node's validator manager
	// Maps Subnets --> validators of the Subnet
	vdrMgr validators.Manager

	// The node's chain manager
	chainManager chains.Manager

	// fee that must be burned by every state creating transaction
	creationTxFee uint64
	// fee that must be burned by every non-state creating transaction
	txFee uint64

	// UptimePercentage is the minimum uptime required to be rewarded for staking.
	uptimePercentage float64

	// The minimum amount of tokens one must bond to be a validator
	minValidatorStake uint64

	// The maximum amount of tokens one can bond to a validator
	maxValidatorStake uint64

	// Minimum stake, in nAVAX, that can be delegated on the primary network
	minDelegatorStake uint64

	// Minimum fee that can be charged for delegation
	minDelegationFee uint32

	// Minimum amount of time to allow a validator to stake
	minStakeDuration time.Duration

	// Maximum amount of time to allow a validator to stake
	maxStakeDuration time.Duration

	// Consumption period for the minting function
	stakeMintingPeriod time.Duration

	// Time of the apricot phase 0 rule change
	apricotPhase0Time time.Time

	dbManager manager.Manager
	// TODO: need to populate
	internalState internalState

	// VersionDB on top of underlying database
	// Important note: In order for writes to [DB] to be persisted,
	// DB.Commit() must be called
	// We use a versionDB here so user can do atomic commits as they see fit
	db database.Database

	// The context of this vm
	ctx *snow.Context

	// ID of the preferred block
	preferred ids.ID

	// ID of the last accepted block
	lastAcceptedID ids.ID

	// channel to send messages to the consensus engine
	toEngine chan<- common.Message

	fx            Fx
	codec         codec.Manager
	codecRegistry codec.Registry

	mempool Mempool

	// Key: block ID
	// Value: the block
	currentBlocks map[ids.ID]Block

	// Contains the IDs of transactions recently dropped because they failed verification.
	// These txs may be re-issued and put into accepted blocks, so check the database
	// to see if it was later committed/aborted before reporting that it's dropped.
	// Key: Tx ID
	// Value: String repr. of the verification error
	droppedTxCache cache.LRU

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped bool

	bootstrappedTime time.Time

	connections map[ids.ShortID]time.Time
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.vdrMgr] must be set before this function is called.
func (vm *VM) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradebytes []byte,
	configBytes []byte,
	msgs chan<- common.Message,
	_ []*common.Fx,
) error {
	ctx.Log.Verbo("initializing platform chain")

	// Initialize metrics as soon as possible
	if err := vm.metrics.Initialize(ctx.Namespace, ctx.Metrics); err != nil {
		return err
	}

	// Initialize the utility to parse addresses
	vm.AddressManager = avax.NewAddressManager(ctx)

	// Initialize the utility to fetch atomic UTXOs
	vm.AtomicUTXOManager = avax.NewAtomicUTXOManager(ctx, Codec)

	// Initialize the utility to track validator uptimes
	vm.Manager = uptime.NewManager(vm.internalState)

	// Initialize the inner VM, which has a lot of boiler-plate logic
	vm.SnowmanVM = &core.SnowmanVM{}
	if err := vm.SnowmanVM.Initialize(ctx, dbManager.Current(), vm.unmarshalBlockFunc, msgs); err != nil {
		return err
	}
	vm.fx = &secp256k1fx.Fx{}

	vm.dbManager = dbManager

	vm.codec = Codec
	vm.codecRegistry = linearcodec.NewDefault()
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}

	vm.droppedTxCache = cache.LRU{Size: droppedTxCacheSize}
	vm.connections = make(map[ids.ShortID]time.Time)

	// Register this VM's types with the database so we can get/put structs to/from it
	vm.registerDBTypes()

	vm.mempool.Initialize(vm)

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if !vm.DBInitialized() {
		genesis := &Genesis{}
		if _, err := GenesisCodec.Unmarshal(genesisBytes, genesis); err != nil {
			return err
		}
		if err := genesis.Initialize(); err != nil {
			return err
		}

		// Persist UTXOs that exist at genesis
		for _, utxo := range genesis.UTXOs {
			if err := vm.putUTXO(vm.DB, &utxo.UTXO); err != nil {
				return err
			}
		}

		// Persist the platform chain's timestamp at genesis
		genesisTime := time.Unix(int64(genesis.Timestamp), 0)
		if err := vm.State.PutTime(vm.DB, timestampKey, genesisTime); err != nil {
			return err
		}

		if err := vm.putCurrentSupply(vm.DB, genesis.InitialSupply); err != nil {
			return err
		}

		// Persist primary network validator set at genesis
		for _, vdrTx := range genesis.Validators {
			var (
				stakeAmount   uint64
				stakeDuration time.Duration
			)
			switch tx := vdrTx.UnsignedTx.(type) {
			case *UnsignedAddValidatorTx:
				stakeAmount = tx.Validator.Wght
				stakeDuration = tx.Validator.Duration()
			default:
				return errWrongTxType
			}
			reward, err := vm.calculateReward(vm.DB, stakeDuration, stakeAmount)
			if err != nil {
				return err
			}
			tx := rewardTx{
				Reward: reward,
				Tx:     *vdrTx,
			}
			if err := vm.addStaker(vm.DB, constants.PrimaryNetworkID, &tx); err != nil {
				return err
			}
		}

		// Persist the subnets that exist at genesis (none do)
		if err := vm.putSubnets(vm.DB, []*Tx{}); err != nil {
			return fmt.Errorf("error putting genesis subnets: %v", err)
		}

		// Ensure all chains that the genesis bytes say to create
		// have the right network ID
		filteredChains := []*Tx{}
		for _, chain := range genesis.Chains {
			unsignedChain, ok := chain.UnsignedTx.(*UnsignedCreateChainTx)
			if !ok {
				vm.Ctx.Log.Warn("invalid tx type in genesis chains")
				continue
			}
			if unsignedChain.NetworkID != vm.Ctx.NetworkID {
				vm.Ctx.Log.Warn("chain has networkID %d, expected %d", unsignedChain.NetworkID, vm.Ctx.NetworkID)
				continue
			}
			filteredChains = append(filteredChains, chain)
		}

		// Persist the chains that exist at genesis
		if err := vm.putChains(vm.DB, filteredChains); err != nil {
			return err
		}

		// Create the genesis block and save it as being accepted (We don't just
		// do genesisBlock.Accept() because then it'd look for genesisBlock's
		// non-existent parent)
		genesisID := hashing.ComputeHash256Array(genesisBytes)
		genesisBlock, err := vm.newCommitBlock(genesisID, 0)
		if err != nil {
			return err
		}
		if err := vm.State.PutBlock(vm.DB, genesisBlock); err != nil {
			return err
		}
		genesisBlock.onAcceptDB = versiondb.New(vm.DB)
		if err := genesisBlock.CommonBlock.Accept(); err != nil {
			return fmt.Errorf("error accepting genesis block: %w", err)
		}

		if err := vm.SetDBInitialized(); err != nil {
			return fmt.Errorf("error while setting db to initialized: %w", err)
		}

		if err := vm.DB.Commit(); err != nil {
			return err
		}
	}

	vm.currentBlocks = make(map[ids.ID]Block)

	if err := vm.updateVdrMgr(true); err != nil {
		ctx.Log.Error("failed to initialize validator sets: %s", err)
		return err
	}

	// Create all of the chains that the database says exist
	if err := vm.initBlockchains(); err != nil {
		vm.Ctx.Log.Warn("could not retrieve existing chains from database: %s", err)
		return err
	}

	lastAcceptedID, err := vm.LastAccepted()
	if err != nil {
		vm.Ctx.Log.Error("Error fetching the last accepted block ID (%s), %s", lastAcceptedID, err)
		return err
	}
	vm.Ctx.Log.Info("initializing last accepted block as %s", lastAcceptedID)

	// Build off the most recently accepted block
	if err := vm.SetPreference(lastAcceptedID); err != nil {
		vm.Ctx.Log.Error("Error setting the preference to the last accepted block (%s), %s", lastAcceptedID, err)
		return err
	}

	// Sanity check to make sure the DB is in a valid state
	lastAcceptedIntf, err := vm.getBlock(lastAcceptedID)
	if err != nil {
		vm.Ctx.Log.Error("Error fetching the last accepted block (%s), %s", vm.Preferred(), err)
		return err
	}
	if _, ok := lastAcceptedIntf.(decision); !ok {
		vm.Ctx.Log.Fatal("The last accepted block, %s, must always be a decision block", lastAcceptedID)
		return errInvalidLastAcceptedBlock
	}

	return nil
}

// Create all chains that exist that this node validates. Should only be called
// after the validator sets are initialized.
func (vm *VM) initBlockchains() error {
	blockchains, err := vm.getChains(vm.DB) // get blockchains that exist
	if err != nil {
		return err
	}

	for _, chain := range blockchains {
		vm.createChain(chain)
	}
	return nil
}

// Create the blockchain described in [tx], but only if this node is a member of
// the Subnet that validates the chain
func (vm *VM) createChain(tx *Tx) {
	unsignedTx, ok := tx.UnsignedTx.(*UnsignedCreateChainTx)
	if !ok {
		// Invalid tx type
		return
	}
	// The validators that compose the Subnet that validates this chain
	validators, subnetExists := vm.vdrMgr.GetValidators(unsignedTx.SubnetID)
	if !subnetExists {
		vm.Ctx.Log.Error("blockchain %s validated by Subnet %s but couldn't get that Subnet. Blockchain not created",
			tx.ID(), unsignedTx.SubnetID)
		return
	}
	if vm.stakingEnabled && // Staking is enabled, so nodes might not validate all chains
		constants.PrimaryNetworkID != unsignedTx.SubnetID && // All nodes must validate the primary network
		!validators.Contains(vm.Ctx.NodeID) { // This node doesn't validate this blockchain
		return
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
	vm.chainManager.CreateChain(chainParams)
}

// Bootstrapping marks this VM as bootstrapping
func (vm *VM) Bootstrapping() error {
	vm.bootstrapped = false
	return vm.fx.Bootstrapping()
}

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error {
	vm.bootstrapped = true

	errs := wrappers.Errs{}
	errs.Add(
		vm.updateVdrMgr(false),
		vm.fx.Bootstrapped(),
	)
	if errs.Errored() {
		return errs.Err
	}

	// TODO: get validator list
	if err := vm.StartTracking(nil); err != nil {
		return err
	}
	return vm.internalState.Commit()
}

// Shutdown this blockchain
func (vm *VM) Shutdown() error {
	if vm.db == nil {
		return nil
	}

	vm.mempool.Shutdown()

	// TODO: get validator list
	if err := vm.Manager.Shutdown(nil); err != nil {
		return err
	}

	errs := wrappers.Errs{}
	errs.Add(
		vm.internalState.Commit(),
		vm.internalState.Close(),
		vm.dbManager.Close(),
	)
	return errs.Err
}

// BuildBlock builds a block to be added to consensus
func (vm *VM) BuildBlock() (snowman.Block, error) { return vm.mempool.BuildBlock() }

// ParseBlock implements the snowman.ChainVM interface
func (vm *VM) ParseBlock(bytes []byte) (snowman.Block, error) {
	blockInterface, err := vm.unmarshalBlockFunc(bytes)
	if err != nil {
		return nil, errors.New("problem parsing block")
	}
	block, ok := blockInterface.(snowman.Block)
	if !ok { // in practice should never happen because unmarshalBlockFunc returns a snowman.Block
		return nil, errors.New("problem parsing block")
	}
	if block, err := vm.GetBlock(block.ID()); err == nil {
		// If we have seen this block before, return it with the most up-to-date info
		return block, nil
	}
	if err := vm.State.PutBlock(vm.DB, block); err != nil { // Persist the block
		return nil, fmt.Errorf("failed to put block due to %w", err)
	}

	return block, vm.DB.Commit()
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

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(blkID ids.ID) error {
	if blkID == vm.preferred {
		return nil
	}

	if err := vm.SnowmanVM.SetPreference(blkID); err != nil {
		return err
	}
	vm.mempool.ResetTimer()
	return nil
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(&Service{vm: vm}, "platform"); err != nil {
		return nil, err
	}

	return map[string]*common.HTTPHandler{
		"": &common.HTTPHandler{
			Handler: server,
		},
	}, nil
}

// CreateStaticHandlers implements the snowman.ChainVM interface
func (vm *VM) CreateStaticHandlers() (map[string]*common.HTTPHandler, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(&StaticService{}, "platform"); err != nil {
		return nil, err
	}

	return map[string]*common.HTTPHandler{
		"": &common.HTTPHandler{
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

func (vm *VM) updateVdrMgr(force bool) error {
	if !force && !vm.bootstrapped {
		return nil
	}

	subnets, err := vm.getSubnets(vm.DB)
	if err != nil {
		return err
	}

	if err := vm.updateVdrSet(constants.PrimaryNetworkID); err != nil {
		return err
	}
	for _, subnet := range subnets {
		if err := vm.updateVdrSet(subnet.ID()); err != nil {
			return err
		}
	}
	return vm.initBlockchains()
}

func (vm *VM) updateVdrSet(subnetID ids.ID) error {
	vdrs := validators.NewSet()

	stopPrefix := []byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix))
	stopDB := prefixdb.NewNested(stopPrefix, vm.DB)
	defer stopDB.Close()
	stopIter := stopDB.NewIterator()
	defer stopIter.Release()

	for stopIter.Next() { // Iterates in order of increasing stop time
		txBytes := stopIter.Value()

		tx := rewardTx{}
		if _, err := vm.codec.Unmarshal(txBytes, &tx); err != nil {
			return fmt.Errorf("couldn't unmarshal validator tx: %w", err)
		}
		if err := tx.Tx.Sign(vm.codec, nil); err != nil {
			return err
		}

		var err error
		switch staker := tx.Tx.UnsignedTx.(type) {
		case *UnsignedAddDelegatorTx:
			err = vdrs.AddWeight(staker.Validator.NodeID, staker.Validator.Weight())
		case *UnsignedAddValidatorTx:
			err = vdrs.AddWeight(staker.Validator.NodeID, staker.Validator.Weight())
		case *UnsignedAddSubnetValidatorTx:
			err = vdrs.AddWeight(staker.Validator.NodeID, staker.Validator.Weight())
		default:
			err = fmt.Errorf("expected validator but got %T", tx.Tx.UnsignedTx)
		}
		if err != nil {
			return err
		}
	}

	if subnetID == constants.PrimaryNetworkID {
		vm.totalStake.Set(float64(vdrs.Weight()) / float64(units.Avax))
	}

	errs := wrappers.Errs{}
	errs.Add(
		vm.vdrMgr.Set(subnetID, vdrs),
		stopIter.Error(),
		stopDB.Close(),
	)
	return errs.Err
}

// Returns the time when the next staker of any subnet starts/stops staking
// after the current timestamp
func (vm *VM) nextStakerChangeTime(vs versionedState) (time.Time, error) {
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

// Codec ...
func (vm *VM) Codec() codec.Manager { return vm.codec }

// CodecRegistry ...
func (vm *VM) CodecRegistry() codec.Registry { return vm.codecRegistry }

// Clock ...
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Logger ...
func (vm *VM) Logger() logging.Logger { return vm.ctx.Log }

// Returns the percentage of the total stake on the Primary Network of nodes
// connected to this node.
func (vm *VM) getPercentConnected() (float64, error) {
	vdrSet, exists := vm.vdrMgr.GetValidators(constants.PrimaryNetworkID)
	if !exists {
		return 0, errNoPrimaryValidators
	}

	vdrs := vdrSet.List()

	var (
		connectedStake uint64
		err            error
	)
	for _, vdr := range vdrs {
		if _, connected := vm.connections[vdr.ID()]; !connected {
			continue // not connected to us --> don't include
		}
		connectedStake, err = safemath.Add64(connectedStake, vdr.Weight())
		if err != nil {
			return 0, err
		}
	}
	return float64(connectedStake) / float64(vdrSet.Weight()), nil
}
