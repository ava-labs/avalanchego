// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/platformvm/uptime"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	currentPrefix         = []byte("current")
	pendingPrefix         = []byte("pending")
	validatorPrefix       = []byte("validator")
	delegatorPrefix       = []byte("delegator")
	subnetValidatorPrefix = []byte("subnetValidator")

	errWrongNetworkID = errors.New("tx has wrong network ID")

	_ InternalState = &internalStateImpl{}
)

const (
	// priority values are used as part of the keys in the pending/current
	// validator state to ensure they are sorted in the order that they should
	// be added/removed.
	lowPriority byte = iota
	mediumPriority
	topPriority
)

type InternalValidatorState interface {
	ValidatorState
	uptime.State

	AddCurrentStaker(tx *Tx, potentialReward uint64)
	DeleteCurrentStaker(tx *Tx)

	AddPendingStaker(tx *Tx)
	DeletePendingStaker(tx *Tx)

	SetCurrentStakerChainState(currentStakerChainState)
	SetPendingStakerChainState(pendingStakerChainState)

	Abort()
	Commit() error
	Close() error
}

/*
 * db
 * |-. current
 * | |-. validator
 * | | '-. list
 * | |   '-- txID -> uptime + potential reward
 * | |-. delegator
 * | | '-. list
 * | |   '-- txID -> potential reward
 * | '-. subnetValidator
 * |   '-. list
 * |     '-- txID -> nil
 * '-. pending
 *   |-. validator
 *   | '-. list
 *   |   '-- txID -> nil
 *   |-. delegator
 *   | '-. list
 *   |   '-- txID -> nil
 *   '-. subnetValidator
 *     '-. list
 *       '-- txID -> nil
 */
type internalValidatorStateImpl struct {
	vm *VM

	currentStakerChainState currentStakerChainState
	pendingStakerChainState pendingStakerChainState

	addedCurrentStakers   []*validatorReward
	deletedCurrentStakers []*Tx
	addedPendingStakers   []*Tx
	deletedPendingStakers []*Tx
	uptimes               map[ids.ShortID]*currentValidatorState // nodeID -> uptimes
	updatedUptimes        map[ids.ShortID]struct{}               // nodeID -> nil

	validatorsDB                 database.Database
	currentValidatorsDB          database.Database
	currentValidatorBaseDB       database.Database
	currentValidatorList         linkeddb.LinkedDB
	currentDelegatorBaseDB       database.Database
	currentDelegatorList         linkeddb.LinkedDB
	currentSubnetValidatorBaseDB database.Database
	currentSubnetValidatorList   linkeddb.LinkedDB
	pendingValidatorsDB          database.Database
	pendingValidatorBaseDB       database.Database
	pendingValidatorList         linkeddb.LinkedDB
	pendingDelegatorBaseDB       database.Database
	pendingDelegatorList         linkeddb.LinkedDB
	pendingSubnetValidatorBaseDB database.Database
	pendingSubnetValidatorList   linkeddb.LinkedDB
}

func newInternalValidatorStateDatabases(vm *VM, db database.Database) *internalValidatorStateImpl {
	currentValidatorsDB := prefixdb.New(currentPrefix, db)
	currentValidatorBaseDB := prefixdb.New(validatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(delegatorPrefix, currentValidatorsDB)
	currentSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(pendingPrefix, db)
	pendingValidatorBaseDB := prefixdb.New(validatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(delegatorPrefix, pendingValidatorsDB)
	pendingSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, pendingValidatorsDB)
	return &internalValidatorStateImpl{
		vm: vm,

		uptimes:        make(map[ids.ShortID]*currentValidatorState),
		updatedUptimes: make(map[ids.ShortID]struct{}),

		currentValidatorsDB:          currentValidatorsDB,
		currentValidatorBaseDB:       currentValidatorBaseDB,
		currentValidatorList:         linkeddb.NewDefault(currentValidatorBaseDB),
		currentDelegatorBaseDB:       currentDelegatorBaseDB,
		currentDelegatorList:         linkeddb.NewDefault(currentDelegatorBaseDB),
		currentSubnetValidatorBaseDB: currentSubnetValidatorBaseDB,
		currentSubnetValidatorList:   linkeddb.NewDefault(currentSubnetValidatorBaseDB),
		pendingValidatorsDB:          pendingValidatorsDB,
		pendingValidatorBaseDB:       pendingValidatorBaseDB,
		pendingValidatorList:         linkeddb.NewDefault(pendingValidatorBaseDB),
		pendingDelegatorBaseDB:       pendingDelegatorBaseDB,
		pendingDelegatorList:         linkeddb.NewDefault(pendingDelegatorBaseDB),
		pendingSubnetValidatorBaseDB: pendingSubnetValidatorBaseDB,
		pendingSubnetValidatorList:   linkeddb.NewDefault(pendingSubnetValidatorBaseDB),
	}
}

func (st *internalStateImpl) CurrentStakerChainState() currentStakerChainState {
	return st.currentStakerChainState
}

func (st *internalStateImpl) PendingStakerChainState() pendingStakerChainState {
	return st.pendingStakerChainState
}

func (st *internalStateImpl) SetCurrentStakerChainState(cs currentStakerChainState) {
	st.currentStakerChainState = cs
}

func (st *internalStateImpl) SetPendingStakerChainState(ps pendingStakerChainState) {
	st.pendingStakerChainState = ps
}

func (st *internalStateImpl) GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	uptime, exists := st.uptimes[nodeID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return uptime.UpDuration, uptime.lastUpdated, nil
}

func (st *internalStateImpl) SetUptime(nodeID ids.ShortID, upDuration time.Duration, lastUpdated time.Time) error {
	uptime, exists := st.uptimes[nodeID]
	if !exists {
		return database.ErrNotFound
	}
	uptime.UpDuration = upDuration
	uptime.lastUpdated = lastUpdated
	st.updatedUptimes[nodeID] = struct{}{}
	return nil
}

func (st *internalStateImpl) AddCurrentStaker(tx *Tx, potentialReward uint64) {
	st.addedCurrentStakers = append(st.addedCurrentStakers, &validatorReward{
		addStakerTx:     tx,
		potentialReward: potentialReward,
	})
}

func (st *internalStateImpl) DeleteCurrentStaker(tx *Tx) {
	st.deletedCurrentStakers = append(st.deletedCurrentStakers, tx)
}

func (st *internalStateImpl) AddPendingStaker(tx *Tx) {
	st.addedPendingStakers = append(st.addedPendingStakers, tx)
}

func (st *internalStateImpl) DeletePendingStaker(tx *Tx) {
	st.deletedPendingStakers = append(st.deletedPendingStakers, tx)
}

func (st *internalStateImpl) Abort() {
	st.baseDB.Abort()
}

func (st *internalStateImpl) Commit() error {
	defer st.Abort()
	batch, err := st.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (st *internalStateImpl) CommitBatch() (database.Batch, error) {
	if err := st.writeCurrentStakers(); err != nil {
		return nil, err
	}
	if err := st.writePendingStakers(); err != nil {
		return nil, err
	}
	if err := st.writeUptimes(); err != nil {
		return nil, err
	}
	if err := st.writeBlocks(); err != nil {
		return nil, err
	}
	if err := st.writeTXs(); err != nil {
		return nil, err
	}
	if err := st.writeRewardUTXOs(); err != nil {
		return nil, err
	}
	if err := st.writeUTXOs(); err != nil {
		return nil, err
	}
	if err := st.writeSubnets(); err != nil {
		return nil, err
	}
	if err := st.writeChains(); err != nil {
		return nil, err
	}
	if err := st.writeSingletons(); err != nil {
		return nil, err
	}
	return st.baseDB.CommitBatch()
}

func (st *internalStateImpl) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		st.pendingSubnetValidatorBaseDB.Close(),
		st.pendingDelegatorBaseDB.Close(),
		st.pendingValidatorBaseDB.Close(),
		st.pendingValidatorsDB.Close(),
		st.currentSubnetValidatorBaseDB.Close(),
		st.currentDelegatorBaseDB.Close(),
		st.currentValidatorBaseDB.Close(),
		st.currentValidatorsDB.Close(),
		st.validatorsDB.Close(),
		st.blockDB.Close(),
		st.txDB.Close(),
		st.rewardUTXODB.Close(),
		st.utxoDB.Close(),
		st.subnetBaseDB.Close(),
		st.chainDB.Close(),
		st.singletonDB.Close(),
		st.baseDB.Close(),
	)
	return errs.Err
}

type currentValidatorState struct {
	txID        ids.ID
	lastUpdated time.Time

	UpDuration      time.Duration `serialize:"true"`
	LastUpdated     uint64        `serialize:"true"` // Unix time in seconds
	PotentialReward uint64        `serialize:"true"`
}

func (st *internalStateImpl) writeCurrentStakers() error {
	for _, currentStaker := range st.addedCurrentStakers {
		txID := currentStaker.addStakerTx.ID()
		potentialReward := currentStaker.potentialReward

		switch tx := currentStaker.addStakerTx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			startTime := tx.StartTime()
			vdr := &currentValidatorState{
				txID:        txID,
				lastUpdated: startTime,

				UpDuration:      0,
				LastUpdated:     uint64(startTime.Unix()),
				PotentialReward: potentialReward,
			}

			vdrBytes, err := GenesisCodec.Marshal(codecVersion, vdr)
			if err != nil {
				return err
			}

			if err := st.currentValidatorList.Put(txID[:], vdrBytes); err != nil {
				return err
			}
			st.uptimes[tx.Validator.NodeID] = vdr
		case *UnsignedAddDelegatorTx:
			if err := database.PutUInt64(st.currentDelegatorList, txID[:], potentialReward); err != nil {
				return err
			}
		case *UnsignedAddSubnetValidatorTx:
			if err := st.currentSubnetValidatorList.Put(txID[:], nil); err != nil {
				return err
			}
		default:
			return errWrongTxType
		}
	}
	st.addedCurrentStakers = nil

	for _, tx := range st.deletedCurrentStakers {
		var db database.KeyValueWriter
		switch tx := tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			db = st.currentValidatorList
			delete(st.uptimes, tx.Validator.NodeID)
			delete(st.updatedUptimes, tx.Validator.NodeID)
		case *UnsignedAddDelegatorTx:
			db = st.currentDelegatorList
		case *UnsignedAddSubnetValidatorTx:
			db = st.currentSubnetValidatorList
		default:
			return errWrongTxType
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return err
		}
	}
	st.deletedCurrentStakers = nil
	return nil
}

func (st *internalStateImpl) writePendingStakers() error {
	for _, tx := range st.addedPendingStakers {
		var db database.KeyValueWriter
		switch tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			db = st.pendingValidatorList
		case *UnsignedAddDelegatorTx:
			db = st.pendingDelegatorList
		case *UnsignedAddSubnetValidatorTx:
			db = st.pendingSubnetValidatorList
		default:
			return errWrongTxType
		}

		txID := tx.ID()
		if err := db.Put(txID[:], nil); err != nil {
			return err
		}
	}
	st.addedPendingStakers = nil

	for _, tx := range st.deletedPendingStakers {
		var db database.KeyValueWriter
		switch tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			db = st.pendingValidatorList
		case *UnsignedAddDelegatorTx:
			db = st.pendingDelegatorList
		case *UnsignedAddSubnetValidatorTx:
			db = st.pendingSubnetValidatorList
		default:
			return errWrongTxType
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return err
		}
	}
	st.deletedPendingStakers = nil
	return nil
}

func (st *internalStateImpl) writeUptimes() error {
	for nodeID := range st.updatedUptimes {
		delete(st.updatedUptimes, nodeID)

		uptime := st.uptimes[nodeID]
		uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

		uptimeBytes, err := GenesisCodec.Marshal(codecVersion, uptime)
		if err != nil {
			return err
		}

		if err := st.currentValidatorList.Put(uptime.txID[:], uptimeBytes); err != nil {
			return err
		}
	}
	return nil
}

func (st *internalStateImpl) load() error {
	if err := st.loadSingletons(); err != nil {
		return err
	}
	if err := st.loadCurrentValidators(); err != nil {
		return err
	}
	if err := st.loadPendingValidators(); err != nil {
		return err
	}
	return nil
}

func (st *internalStateImpl) loadCurrentValidators() error {
	cs := &currentStakerChainStateImpl{
		validatorsByNodeID: make(map[ids.ShortID]*currentValidatorImpl),
		validatorsByTxID:   make(map[ids.ID]*validatorReward),
	}

	validatorIt := st.currentValidatorList.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		uptimeBytes := validatorIt.Value()
		uptime := &currentValidatorState{
			txID: txID,
		}
		if _, err := GenesisCodec.Unmarshal(uptimeBytes, uptime); err != nil {
			return err
		}
		uptime.lastUpdated = time.Unix(int64(uptime.LastUpdated), 0)

		addValidatorTx, ok := tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			return errWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		cs.validatorsByNodeID[addValidatorTx.Validator.NodeID] = &currentValidatorImpl{
			validatorImpl: validatorImpl{
				subnets: make(map[ids.ID]*UnsignedAddSubnetValidatorTx),
			},
			addValidatorTx:  addValidatorTx,
			potentialReward: uptime.PotentialReward,
		}
		cs.validatorsByTxID[txID] = &validatorReward{
			addStakerTx:     tx,
			potentialReward: uptime.PotentialReward,
		}

		st.uptimes[addValidatorTx.Validator.NodeID] = uptime
	}

	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := st.currentDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		potentialRewardBytes := delegatorIt.Value()
		potentialReward, err := database.ParseUInt64(potentialRewardBytes)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.UnsignedTx.(*UnsignedAddDelegatorTx)
		if !ok {
			return errWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addDelegatorTx.Validator.NodeID]
		if !exists {
			return errDelegatorSubset
		}
		vdr.delegatorWeight += addDelegatorTx.Validator.Wght
		vdr.delegators = append(vdr.delegators, addDelegatorTx)
		cs.validatorsByTxID[txID] = &validatorReward{
			addStakerTx:     tx,
			potentialReward: potentialReward,
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := st.currentSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.UnsignedTx.(*UnsignedAddSubnetValidatorTx)
		if !ok {
			return errWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addSubnetValidatorTx.Validator.NodeID]
		if !exists {
			return errDSValidatorSubset
		}
		vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = addSubnetValidatorTx
		cs.validatorsByTxID[txID] = &validatorReward{
			addStakerTx: tx,
		}
	}
	if err := subnetValidatorIt.Error(); err != nil {
		return err
	}

	for _, vdr := range cs.validatorsByNodeID {
		sortDelegatorsByRemoval(vdr.delegators)
	}
	sortValidatorsByRemoval(cs.validators)
	cs.setNextStaker()

	st.currentStakerChainState = cs
	return nil
}

func (st *internalStateImpl) loadPendingValidators() error {
	ps := &pendingStakerChainStateImpl{
		validatorsByNodeID:      make(map[ids.ShortID]*UnsignedAddValidatorTx),
		validatorExtrasByNodeID: make(map[ids.ShortID]*validatorImpl),
	}

	validatorIt := st.pendingValidatorList.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		addValidatorTx, ok := tx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			return errWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		ps.validatorsByNodeID[addValidatorTx.Validator.NodeID] = addValidatorTx
	}
	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := st.pendingDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.UnsignedTx.(*UnsignedAddDelegatorTx)
		if !ok {
			return errWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID]; exists {
			vdr.delegators = append(vdr.delegators, addDelegatorTx)
		} else {
			ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID] = &validatorImpl{
				delegators: []*UnsignedAddDelegatorTx{addDelegatorTx},
				subnets:    make(map[ids.ID]*UnsignedAddSubnetValidatorTx),
			}
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := st.pendingSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.UnsignedTx.(*UnsignedAddSubnetValidatorTx)
		if !ok {
			return errWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID]; exists {
			vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = addSubnetValidatorTx
		} else {
			ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]*UnsignedAddSubnetValidatorTx{
					addSubnetValidatorTx.Validator.Subnet: addSubnetValidatorTx,
				},
			}
		}
	}
	if err := subnetValidatorIt.Error(); err != nil {
		return err
	}

	for _, vdr := range ps.validatorExtrasByNodeID {
		sortDelegatorsByAddition(vdr.delegators)
	}
	sortValidatorsByAddition(ps.validators)

	st.pendingStakerChainState = ps
	return nil
}

func (st *internalStateImpl) init(genesisBytes []byte) error {
	genesis := &Genesis{}
	if _, err := GenesisCodec.Unmarshal(genesisBytes, genesis); err != nil {
		return err
	}
	if err := genesis.Initialize(); err != nil {
		return err
	}

	// Persist UTXOs that exist at genesis
	for _, utxo := range genesis.UTXOs {
		st.AddUTXO(&utxo.UTXO)
	}

	// Persist the platform chain's timestamp at genesis
	genesisTime := time.Unix(int64(genesis.Timestamp), 0)
	st.SetTimestamp(genesisTime)
	st.SetCurrentSupply(genesis.InitialSupply)

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesis.Validators {
		tx, ok := vdrTx.UnsignedTx.(*UnsignedAddValidatorTx)
		if !ok {
			return errWrongTxType
		}

		stakeAmount := tx.Validator.Wght
		stakeDuration := tx.Validator.Duration()
		currentSupply := st.GetCurrentSupply()

		r := reward(
			stakeDuration,
			stakeAmount,
			currentSupply,
			st.vm.StakeMintingPeriod,
		)
		newCurrentSupply, err := safemath.Add64(currentSupply, r)
		if err != nil {
			return err
		}

		st.AddCurrentStaker(vdrTx, r)
		st.AddTx(vdrTx, Committed)
		st.SetCurrentSupply(newCurrentSupply)
	}

	for _, chain := range genesis.Chains {
		unsignedChain, ok := chain.UnsignedTx.(*UnsignedCreateChainTx)
		if !ok {
			return errWrongTxType
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		if unsignedChain.NetworkID != st.vm.ctx.NetworkID {
			return errWrongNetworkID
		}

		st.AddChain(chain)
		st.AddTx(chain, Committed)
	}

	// Create the genesis block and save it as being accepted (We don't just
	// do genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlock, err := st.vm.newCommitBlock(genesisID, 0)
	if err != nil {
		return err
	}
	genesisBlock.status = choices.Accepted
	st.AddBlock(genesisBlock)
	st.SetLastAccepted(genesisBlock.ID())

	if err := st.singletonDB.Put(initializedKey, nil); err != nil {
		return err
	}

	return st.Commit()
}
