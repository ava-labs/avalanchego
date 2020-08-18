// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"math"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/validators"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/utils/units"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/core"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

const (
	// For putting/getting values from state
	validatorsTypeID uint64 = iota
	chainsTypeID
	blockTypeID
	subnetsTypeID
	utxoTypeID
	utxoSetTypeID
	txTypeID
	statusTypeID

	// Delta is the synchrony bound used for safe decision making
	Delta = 10 * time.Second

	// BatchSize is the number of decision transaction to place into a block
	BatchSize = 30

	// NumberOfShares is the number of shares that a delegator is
	// rewarded
	NumberOfShares = 1000000

	// TODO: Turn these constants into governable parameters

	// InflationRate is the inflation rate from staking
	// TODO: make inflation rate non-zero
	InflationRate = 1.00

	// MinimumStakeAmount is the minimum amount of tokens one must bond to be a staker
	MinimumStakeAmount = 10 * units.MicroAvax

	// MinimumStakingDuration is the shortest amount of time a staker can bond
	// their funds for.
	MinimumStakingDuration = 24 * time.Hour

	// MaximumStakingDuration is the longest amount of time a staker can bond
	// their funds for.
	MaximumStakingDuration = 365 * 24 * time.Hour

	droppedTxCacheSize = 50

	maxUTXOsToFetch = 1024
)

var (
	// taken from https://stackoverflow.com/questions/25065055/what-is-the-maximum-time-time-in-go/32620397#32620397
	maxTime = time.Unix(1<<63-62135596801, 0) // 0 is used because we drop the nano-seconds

	timestampKey         = ids.NewID([32]byte{'t', 'i', 'm', 'e'})
	currentValidatorsKey = ids.NewID([32]byte{'c', 'u', 'r', 'r', 'e', 'n', 't'})
	pendingValidatorsKey = ids.NewID([32]byte{'p', 'e', 'n', 'd', 'i', 'n', 'g'})
	chainsKey            = ids.NewID([32]byte{'c', 'h', 'a', 'i', 'n', 's'})
	subnetsKey           = ids.NewID([32]byte{'s', 'u', 'b', 'n', 'e', 't', 's'})
)

var (
	errEndOfTime                = errors.New("program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred")
	errTimeTooAdvanced          = errors.New("this is proposing a time too far in the future")
	errNoPendingBlocks          = errors.New("no pending blocks")
	errUnsupportedFXs           = errors.New("unsupported feature extensions")
	errRegisteringType          = errors.New("error registering type with database")
	errMissingBlock             = errors.New("missing block")
	errInvalidLastAcceptedBlock = errors.New("last accepted block must be a decision block")
	errInvalidAddress           = errors.New("invalid address")
	errInvalidAddressSeperator  = errors.New("invalid address seperator")
	errInvalidAddressPrefix     = errors.New("invalid address prefix")
	errInvalidAddressSuffix     = errors.New("invalid address suffix")
	errEmptyAddressPrefix       = errors.New("empty address prefix")
	errEmptyAddressSuffix       = errors.New("empty address suffix")
	errInvalidID                = errors.New("invalid ID")
	errDSCantValidate           = errors.New("new blockchain can't be validated by default Subnet")
)

// Codec does serialization and deserialization
var Codec codec.Codec

func init() {
	Codec = codec.NewDefault()

	errs := wrappers.Errs{}
	errs.Add(
		Codec.RegisterType(&ProposalBlock{}),
		Codec.RegisterType(&Abort{}),
		Codec.RegisterType(&Commit{}),
		Codec.RegisterType(&StandardBlock{}),
		Codec.RegisterType(&AtomicBlock{}),

		// The Fx is registered here because this is the same place it is
		// registered in the AVM. This ensures that the typeIDs match up for
		// utxos in shared memory.
		Codec.RegisterType(&secp256k1fx.TransferInput{}),
		Codec.RegisterType(&secp256k1fx.MintOutput{}),
		Codec.RegisterType(&secp256k1fx.TransferOutput{}),
		Codec.RegisterType(&secp256k1fx.MintOperation{}),
		Codec.RegisterType(&secp256k1fx.Credential{}),
		Codec.RegisterType(&secp256k1fx.Input{}),
		Codec.RegisterType(&secp256k1fx.OutputOwners{}),

		Codec.RegisterType(&UnsignedAddDefaultSubnetValidatorTx{}),
		Codec.RegisterType(&UnsignedAddNonDefaultSubnetValidatorTx{}),
		Codec.RegisterType(&UnsignedAddDefaultSubnetDelegatorTx{}),

		Codec.RegisterType(&UnsignedCreateChainTx{}),
		Codec.RegisterType(&UnsignedCreateSubnetTx{}),

		Codec.RegisterType(&UnsignedImportTx{}),
		Codec.RegisterType(&UnsignedExportTx{}),

		Codec.RegisterType(&UnsignedAdvanceTimeTx{}),
		Codec.RegisterType(&UnsignedRewardValidatorTx{}),

		Codec.RegisterType(&StakeableLockIn{}),
		Codec.RegisterType(&StakeableLockOut{}),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

// VM implements the snowman.ChainVM interface
type VM struct {
	*core.SnowmanVM

	// Node's validator manager
	// Maps Subnets --> nodes in the Subnet
	validators validators.Manager

	// true if the node is being run with staking enabled
	stakingEnabled bool

	// The node's chain manager
	chainManager chains.Manager

	// AVAX asset ID
	avaxAssetID ids.ID

	// AVM is the ID of the avm virtual machine
	avm ids.ID

	fx    Fx
	codec codec.Codec

	// Used to create and use keys.
	factory crypto.FactorySECP256K1R

	// Used to get time. Useful for faking time during tests.
	clock timer.Clock

	// Key: block ID
	// Value: the block
	currentBlocks map[[32]byte]Block

	// Transactions that have not been put into blocks yet
	unissuedProposalTxs *EventHeap
	unissuedDecisionTxs []*Tx
	unissuedAtomicTxs   []*Tx

	// Tx fee burned by a transaction
	txFee uint64

	// This timer goes off when it is time for the next validator to add/leave the validator set
	// When it goes off resetTimer() is called, triggering creation of a new block
	timer *timer.Timer

	// Contains the IDs of transactions recently dropped because they failed verification.
	// These txs may be re-issued and put into accepted blocks, so check the database
	// to see if it was later committed/aborted before reporting that it's dropped
	droppedTxCache cache.LRU

	// Bootstrapped remembers if this chain has finished bootstrapping or not
	bootstrapped bool
}

// Initialize this blockchain.
// [vm.ChainManager] and [vm.Validators] must be set before this function is called.
func (vm *VM) Initialize(
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	msgs chan<- common.Message,
	_ []*common.Fx,
) error {
	ctx.Log.Verbo("initializing platform chain")
	// Initialize the inner VM, which has a lot of boiler-plate logic
	vm.SnowmanVM = &core.SnowmanVM{}
	if err := vm.SnowmanVM.Initialize(ctx, db, vm.unmarshalBlockFunc, msgs); err != nil {
		return err
	}
	vm.fx = &secp256k1fx.Fx{}

	vm.codec = codec.NewDefault()
	if err := vm.fx.Initialize(vm); err != nil {
		return err
	}
	vm.codec = Codec

	vm.droppedTxCache = cache.LRU{Size: droppedTxCacheSize}

	// Register this VM's types with the database so we can get/put structs to/from it
	vm.registerDBTypes()

	// If the database is empty, create the platform chain anew using
	// the provided genesis state
	if !vm.DBInitialized() {
		genesis := &Genesis{}
		if err := Codec.Unmarshal(genesisBytes, genesis); err != nil {
			return err
		}
		if err := genesis.Initialize(); err != nil {
			return err
		}

		// Persist UTXOs that exist at genesis
		for _, utxo := range genesis.UTXOs {
			if err := vm.putUTXO(vm.DB, utxo); err != nil {
				return err
			}
		}

		// Persist default subnet validator set at genesis
		if err := vm.putCurrentValidators(vm.DB, genesis.Validators, constants.DefaultSubnetID); err != nil {
			return err
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

		// Persist the platform chain's timestamp at genesis
		time := time.Unix(int64(genesis.Timestamp), 0)
		if err := vm.State.PutTime(vm.DB, timestampKey, time); err != nil {
			return err
		}

		// There are no pending stakers at genesis
		if err := vm.putPendingValidators(vm.DB, &EventHeap{SortByStartTime: true}, constants.DefaultSubnetID); err != nil {
			return err
		}

		// Create the genesis block and save it as being accepted (We don't just
		// do genesisBlock.Accept() because then it'd look for genesisBlock's
		// non-existent parent)
		genesisBlock, err := vm.newCommitBlock(ids.Empty, 0)
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

	// Transactions from clients that have not yet been put into blocks
	// and added to consensus
	vm.unissuedProposalTxs = &EventHeap{SortByStartTime: true}

	vm.currentBlocks = make(map[[32]byte]Block)
	vm.timer = timer.NewTimer(func() {
		vm.Ctx.Lock.Lock()
		defer vm.Ctx.Lock.Unlock()

		vm.resetTimer()
	})
	go ctx.Log.RecoverAndPanic(vm.timer.Dispatch)

	if err := vm.initSubnets(); err != nil {
		ctx.Log.Error("failed to initialize Subnets: %s", err)
		return err
	}

	// Create all of the chains that the database says exist
	if err := vm.initBlockchains(); err != nil {
		vm.Ctx.Log.Warn("could not retrieve existing chains from database: %s", err)
		return err
	}

	lastAcceptedID := vm.LastAccepted()
	vm.Ctx.Log.Info("Initializing last accepted block as %s", lastAcceptedID)

	// Build off the most recently accepted block
	vm.SetPreference(lastAcceptedID)

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

// Queue [tx] to be put into a block
func (vm *VM) issueTx(tx *Tx) error {
	// Initialize the transaction
	if err := tx.Sign(vm.codec, nil); err != nil {
		return err
	}
	switch tx.UnsignedTx.(type) {
	case TimedTx:
		vm.unissuedProposalTxs.Add(tx)
	case UnsignedDecisionTx:
		vm.unissuedDecisionTxs = append(vm.unissuedDecisionTxs, tx)
	case UnsignedAtomicTx:
		vm.unissuedAtomicTxs = append(vm.unissuedAtomicTxs, tx)
	default:
		return errors.New("Could not parse given tx. Provided tx needs to be a ProposalTx, DecisionTx, or AtomicTx")
	}
	vm.resetTimer()
	return nil
}

// Create all chains that exist that this node validates
// Can only be called after initSubnets()
func (vm *VM) initBlockchains() error {
	vm.Ctx.Log.Info("initializing blockchains")
	blockchains, err := vm.getChains(vm.DB) // get blockchains that exist
	if err != nil {
		return err
	}

	for _, chain := range blockchains {
		vm.createChain(chain)
	}
	return nil
}

// Set the node's validator manager to be up to date
func (vm *VM) initSubnets() error {
	vm.Ctx.Log.Info("initializing Subnets")
	subnets, err := vm.getSubnets(vm.DB)
	if err != nil {
		return err
	}

	if err := vm.updateValidators(constants.DefaultSubnetID); err != nil {
		return err
	}

	for _, subnet := range subnets {
		if err := vm.updateValidators(subnet.ID()); err != nil {
			return err
		}
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
	validators, subnetExists := vm.validators.GetValidatorSet(unsignedTx.SubnetID)
	if !subnetExists {
		vm.Ctx.Log.Error("blockchain %s validated by Subnet %s but couldn't get that Subnet. Blockchain not created")
		return
	}
	if vm.stakingEnabled && // Staking is enabled, so nodes might not validate all chains
		!constants.DefaultSubnetID.Equals(unsignedTx.SubnetID) && // All nodes must validate the default subnet
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
func (vm *VM) Bootstrapping() error { vm.bootstrapped = false; return vm.fx.Bootstrapping() }

// Bootstrapped marks this VM as bootstrapped
func (vm *VM) Bootstrapped() error { vm.bootstrapped = true; return vm.fx.Bootstrapped() }

// Shutdown this blockchain
func (vm *VM) Shutdown() error {
	if vm.timer == nil {
		return nil
	}

	// There is a potential deadlock if the timer is about to execute a timeout.
	// So, the lock must be released before stopping the timer.
	vm.Ctx.Lock.Unlock()
	vm.timer.Stop()
	vm.Ctx.Lock.Lock()

	return vm.DB.Close()
}

// BuildBlock builds a block to be added to consensus
func (vm *VM) BuildBlock() (snowman.Block, error) {
	vm.Ctx.Log.Debug("in BuildBlock")
	// TODO: Add PreferredHeight() to core.snowmanVM
	preferredHeight, err := vm.preferredHeight()
	if err != nil {
		return nil, fmt.Errorf("couldn't get preferred block's height: %w", err)
	}

	preferredID := vm.Preferred()

	// If there are pending decision txs, build a block with a batch of them
	if len(vm.unissuedDecisionTxs) > 0 {
		numTxs := BatchSize
		if numTxs > len(vm.unissuedDecisionTxs) {
			numTxs = len(vm.unissuedDecisionTxs)
		}
		var txs []*Tx
		txs, vm.unissuedDecisionTxs = vm.unissuedDecisionTxs[:numTxs], vm.unissuedDecisionTxs[numTxs:]
		blk, err := vm.newStandardBlock(preferredID, preferredHeight+1, txs)
		if err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := blk.Verify(); err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			vm.resetTimer()
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// If there is a pending atomic tx, build a block with it
	if len(vm.unissuedAtomicTxs) > 0 {
		tx := vm.unissuedAtomicTxs[0]
		vm.unissuedAtomicTxs = vm.unissuedAtomicTxs[1:]
		blk, err := vm.newAtomicBlock(preferredID, preferredHeight+1, *tx)
		if err != nil {
			return nil, err
		}
		if err := blk.Verify(); err != nil {
			vm.resetTimer()
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// Get the preferred block (which we want to build off)
	preferred, err := vm.getBlock(preferredID)
	vm.Ctx.Log.AssertNoError(err)

	// The database if the preferred block were to be accepted
	var db database.Database
	// The preferred block should always be a decision block
	if preferred, ok := preferred.(decision); ok {
		db = preferred.onAccept()
	} else {
		return nil, errInvalidBlockType
	}

	// The chain time if the preferred block were to be committed
	currentChainTimestamp, err := vm.getTimestamp(db)
	if err != nil {
		return nil, err
	}
	if !currentChainTimestamp.Before(maxTime) {
		return nil, errEndOfTime
	}

	// If the chain time would be the time for the next default subnet validator to leave,
	// then we create a block that removes the validator and proposes they receive a validator reward
	currentValidators, err := vm.getCurrentValidators(db, constants.DefaultSubnetID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get validator set: %w", err)
	}
	nextValidatorEndtime := maxTime
	if currentValidators.Len() > 0 {
		nextValidatorEndtime = currentValidators.Peek().UnsignedTx.(TimedTx).EndTime()
	}
	if currentChainTimestamp.Equal(nextValidatorEndtime) {
		stakerTx := currentValidators.Peek()
		rewardValidatorTx, err := vm.newRewardValidatorTx(stakerTx.ID())
		if err != nil {
			return nil, err
		}
		blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *rewardValidatorTx)
		if err != nil {
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// If local time is >= time of the next validator set change,
	// propose moving the chain time forward
	nextValidatorStartTime := vm.nextValidatorChangeTime(db /*start=*/, true)
	nextValidatorEndTime := vm.nextValidatorChangeTime(db /*start=*/, false)

	nextValidatorSetChangeTime := nextValidatorStartTime
	if nextValidatorEndTime.Before(nextValidatorStartTime) {
		nextValidatorSetChangeTime = nextValidatorEndTime
	}

	localTime := vm.clock.Time()
	if !localTime.Before(nextValidatorSetChangeTime) { // time is at or after the time for the next validator to join/leave
		advanceTimeTx, err := vm.newAdvanceTimeTx(nextValidatorSetChangeTime)
		if err != nil {
			return nil, err
		}
		blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *advanceTimeTx)
		if err != nil {
			return nil, err
		}
		if err := vm.State.PutBlock(vm.DB, blk); err != nil {
			return nil, err
		}
		return blk, vm.DB.Commit()
	}

	// Propose adding a new validator but only if their start time is in the
	// future relative to local time (plus Delta)
	syncTime := localTime.Add(Delta)
	for vm.unissuedProposalTxs.Len() > 0 {
		tx := vm.unissuedProposalTxs.Remove()
		utx := tx.UnsignedTx.(TimedTx)
		if !syncTime.After(utx.StartTime()) {
			blk, err := vm.newProposalBlock(preferredID, preferredHeight+1, *tx)
			if err != nil {
				return nil, err
			}
			if err := vm.State.PutBlock(vm.DB, blk); err != nil {
				return nil, err
			}
			return blk, vm.DB.Commit()
		}
		vm.Ctx.Log.Debug("dropping tx to add validator because start time too late")
	}

	vm.Ctx.Log.Debug("BuildBlock returning error (no blocks)")
	return nil, errNoPendingBlocks
}

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
	if blk, exists := vm.currentBlocks[blkID.Key()]; exists {
		return blk, nil
	}
	// Block isn't in memory. If block is in database, return it.
	blkInterface, err := vm.State.GetBlock(vm.DB, blkID)
	if err != nil {
		return nil, err
	}
	if block, ok := blkInterface.(Block); ok {
		return block, nil
	}
	return nil, errors.New("block not found")
}

// SetPreference sets the preferred block to be the one with ID [blkID]
func (vm *VM) SetPreference(blkID ids.ID) {
	if !blkID.Equals(vm.Preferred()) {
		vm.SnowmanVM.SetPreference(blkID)
		vm.resetTimer()
	}
}

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	// Create a service with name "platform"
	handler, err := vm.SnowmanVM.NewHandler("platform", &Service{vm: vm})
	vm.Ctx.Log.AssertNoError(err)
	return map[string]*common.HTTPHandler{"": handler}
}

// CreateStaticHandlers implements the snowman.ChainVM interface
func (vm *VM) CreateStaticHandlers() map[string]*common.HTTPHandler {
	// Static service's name is platform
	handler, _ := vm.SnowmanVM.NewHandler("platform", &StaticService{})
	return map[string]*common.HTTPHandler{
		"": handler,
	}
}

// Check if there is a block ready to be added to consensus
// If so, notify the consensus engine
func (vm *VM) resetTimer() {
	// If there is a pending transaction, trigger building of a block with that
	// transaction
	if len(vm.unissuedDecisionTxs) > 0 || len(vm.unissuedAtomicTxs) > 0 {
		vm.SnowmanVM.NotifyBlockReady()
		return
	}

	// Get the preferred block
	preferredIntf, err := vm.getBlock(vm.Preferred())
	if err != nil {
		vm.Ctx.Log.Error("Error fetching the preferred block (%s), %s", vm.Preferred(), err)
		return
	}

	// The database if the preferred block were to be committed
	var db database.Database
	// The preferred block should always be a decision block
	if preferred, ok := preferredIntf.(decision); ok {
		db = preferred.onAccept()
	} else {
		vm.Ctx.Log.Error("The preferred block, %s, should always be a decision block", vm.Preferred())
		return
	}

	// The chain time if the preferred block were to be committed
	timestamp, err := vm.getTimestamp(db)
	if err != nil {
		vm.Ctx.Log.Error("could not retrieve timestamp from database")
		return
	}
	if timestamp.Equal(maxTime) {
		vm.Ctx.Log.Error("Program time is suspiciously far in the future. Either this codebase was way more successful than expected, or a critical error has occurred.")
		return
	}

	nextDSValidatorEndTime := vm.nextSubnetValidatorChangeTime(db, constants.DefaultSubnetID, false)
	if timestamp.Equal(nextDSValidatorEndTime) {
		vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeRewardValidator
		return
	}

	// If local time is >= time of the next change in the validator set,
	// propose moving forward the chain timestamp
	nextValidatorStartTime := vm.nextValidatorChangeTime(db, true)
	nextValidatorEndTime := vm.nextValidatorChangeTime(db, false)

	nextValidatorSetChangeTime := nextValidatorStartTime
	if nextValidatorEndTime.Before(nextValidatorStartTime) {
		nextValidatorSetChangeTime = nextValidatorEndTime
	}

	localTime := vm.clock.Time()
	if !localTime.Before(nextValidatorSetChangeTime) { // time is at or after the time for the next validator to join/leave
		vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeTimestamp
		return
	}

	syncTime := localTime.Add(Delta)
	for vm.unissuedProposalTxs.Len() > 0 {
		if !syncTime.After(vm.unissuedProposalTxs.Peek().UnsignedTx.(TimedTx).StartTime()) {
			vm.SnowmanVM.NotifyBlockReady() // Should issue a ProposeAddValidator
			return
		}
		// If the tx doesn't meet the synchrony bound, drop it
		vm.unissuedProposalTxs.Remove()
		vm.Ctx.Log.Debug("dropping tx to add validator because its start time has passed")
	}

	waitTime := nextValidatorSetChangeTime.Sub(localTime)
	vm.Ctx.Log.Debug("next scheduled event is at %s (%s in the future)", nextValidatorSetChangeTime, waitTime)

	// Wake up when it's time to add/remove the next validator
	vm.timer.SetTimeoutIn(waitTime)
}

// If [start], returns the time at which the next validator (of any subnet) in the pending set starts validating
// Otherwise, returns the time at which the next validator (of any subnet) stops validating
// If no such validator is found, returns maxTime
func (vm *VM) nextValidatorChangeTime(db database.Database, start bool) time.Time {
	earliest := vm.nextSubnetValidatorChangeTime(db, constants.DefaultSubnetID, start)
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return earliest
	}
	for _, subnet := range subnets {
		t := vm.nextSubnetValidatorChangeTime(db, subnet.ID(), start)
		if t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

func (vm *VM) nextSubnetValidatorChangeTime(db database.Database, subnetID ids.ID, start bool) time.Time {
	var validators *EventHeap
	var err error
	if start {
		validators, err = vm.getPendingValidators(db, subnetID)
	} else {
		validators, err = vm.getCurrentValidators(db, subnetID)
	}
	if err != nil {
		vm.Ctx.Log.Error("couldn't get validators of subnet with ID %s: %v", subnetID, err)
		return maxTime
	}
	if validators.Len() == 0 {
		vm.Ctx.Log.Verbo("subnet, %s, has no validators", subnetID)
		return maxTime
	}
	return validators.Timestamp()
}

// Returns:
// 1) The validator set of subnet with ID [subnetID] when timestamp is advanced to [timestamp]
// 2) The pending validator set of subnet with ID [subnetID] when timestamp is advanced to [timestamp]
// 3) The IDs of the validators that start validating [subnetID] between now and [timestamp]
// 4) The IDs of the validators that stop validating [subnetID] between now and [timestamp]
// Note that this method will not remove validators from the current validator set of the default subnet.
// That happens in reward blocks.
func (vm *VM) calculateValidators(db database.Database, timestamp time.Time, subnetID ids.ID) (current,
	pending *EventHeap, started, stopped ids.ShortSet, err error) {
	// remove validators whose end time <= [timestamp]
	current, err = vm.getCurrentValidators(db, subnetID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !subnetID.Equals(constants.DefaultSubnetID) { // validators of default subnet removed in rewardValidatorTxs, not here
		for current.Len() > 0 {
			next := current.Peek().UnsignedTx.(*UnsignedAddNonDefaultSubnetValidatorTx) // current validator with earliest end time
			if timestamp.Before(next.EndTime()) {
				break
			}
			current.Remove()
			stopped.Add(next.Validator.Vdr().ID())
		}
	}
	pending, err = vm.getPendingValidators(db, subnetID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	for pending.Len() > 0 {
		nextTx := pending.Peek() // pending staker with earliest start time
		switch tx := nextTx.UnsignedTx.(type) {
		case *UnsignedAddDefaultSubnetValidatorTx:
			if timestamp.Before(tx.StartTime()) {
				break
			}
			current.Add(nextTx)
			pending.Remove()
			started.Add(tx.Validator.Vdr().ID())
		case *UnsignedAddNonDefaultSubnetValidatorTx:
			if timestamp.Before(tx.StartTime()) {
				break
			}
			current.Add(nextTx)
			pending.Remove()
			started.Add(tx.Validator.Vdr().ID())
		case *UnsignedAddDefaultSubnetDelegatorTx:
			if timestamp.Before(tx.StartTime()) {
				break
			}
			current.Add(nextTx)
			pending.Remove()
			started.Add(tx.Validator.Vdr().ID())
		default:
			pending.Remove()
		}
	}
	return current, pending, started, stopped, nil
}

func (vm *VM) getValidators(validatorEvents *EventHeap) []validators.Validator {
	vdrMap := make(map[[20]byte]*Validator, validatorEvents.Len())
	for _, event := range validatorEvents.Txs {
		var vdr validators.Validator
		switch tx := event.UnsignedTx.(type) {
		case *UnsignedAddDefaultSubnetValidatorTx:
			vdr = tx.Validator.Vdr()
		case *UnsignedAddDefaultSubnetDelegatorTx:
			vdr = tx.Validator.Vdr()
		case *UnsignedAddNonDefaultSubnetValidatorTx:
			vdr = tx.Validator.Vdr()
		default:
			continue
		}
		vdrID := vdr.ID()
		vdrKey := vdrID.Key()
		validator, exists := vdrMap[vdrKey]
		if !exists {
			validator = &Validator{NodeID: vdrID}
			vdrMap[vdrKey] = validator
		}
		weight, err := safemath.Add64(validator.Wght, vdr.Weight())
		if err != nil {
			weight = math.MaxUint64
		}
		validator.Wght = weight
	}

	vdrList := make([]validators.Validator, len(vdrMap))
	i := 0
	for _, validator := range vdrMap {
		vdrList[i] = validator
		i++
	}
	return vdrList
}

// update the node's validator manager to contain the current validator set of the given Subnet
func (vm *VM) updateValidators(subnetID ids.ID) error {
	validatorSet, subnetInitialized := vm.validators.GetValidatorSet(subnetID)
	if !subnetInitialized { // validator manager doesn't know about this subnet yet
		validatorSet = validators.NewSet()
		vm.validators.PutValidatorSet(subnetID, validatorSet)
	}

	currentValidators, err := vm.getCurrentValidators(vm.DB, subnetID)
	if err != nil {
		return err
	}

	validators := vm.getValidators(currentValidators)
	return validatorSet.Set(validators)
}

// Codec ...
func (vm *VM) Codec() codec.Codec { return vm.codec }

// Clock ...
func (vm *VM) Clock() *timer.Clock { return &vm.clock }

// Logger ...
func (vm *VM) Logger() logging.Logger { return vm.Ctx.Log }

// GetAtomicUTXOs returns imported/exports UTXOs such that at least one of the addresses in [addrs] is referenced.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Returns:
// * The fetched of UTXOs
// * true if all there are no more UTXOs in this range to fetch
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) GetAtomicUTXOs(
	chainID ids.ID,
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch {
		limit = maxUTXOsToFetch
	}

	seen := ids.Set{} // IDs of UTXOs already in the list
	utxos := make([]*avax.UTXO, 0, limit)
	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)
	smDB := vm.Ctx.SharedMemory.GetDatabase(chainID)
	defer vm.Ctx.SharedMemory.ReleaseDatabase(chainID)
	state := avax.NewPrefixedState(smDB, vm.codec, vm.Ctx.ChainID, chainID)
	for _, addr := range addrs.List() {
		if bytes.Compare(addr.Bytes(), startAddr.Bytes()) < 0 { // Skip addresses before start
			continue
		}
		utxoIDs, err := state.Funds(addr.Bytes(), startUTXOID, limit)
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s", addr)
		}
		for _, utxoID := range utxoIDs {
			if seen.Contains(utxoID) { // Already have this UTXO in the list
				continue
			}
			utxo, err := state.UTXO(utxoID)
			if err != nil {
				return nil, ids.ShortEmpty, ids.Empty, err
			}
			utxos = append(utxos, utxo)
			seen.Add(utxoID)
			lastAddr = addr
			lastIndex = utxoID
			limit--
			if limit <= 0 { // We fetched enough UTXOs; stop.
				break
			}
		}
	}
	return utxos, lastAddr, lastIndex, nil
}

// ParseLocalAddress takes in an address for this chain and produces the ID
func (vm *VM) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := vm.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if !chainID.Equals(vm.Ctx.ChainID) {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			vm.Ctx.ChainID, chainID)
	}
	return addr, nil
}

// ParseAddress takes in an address and produces the ID of the chain it's for
// the ID of the address
func (vm *VM) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := formatting.ParseAddress(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := vm.Ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(vm.Ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q",
			expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func (vm *VM) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return vm.FormatAddress(vm.Ctx.ChainID, addr)
}

// FormatAddress takes in a chainID and a raw address and produces the formatted
// address
func (vm *VM) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := vm.Ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(vm.Ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}
