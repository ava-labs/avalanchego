// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/uptime"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	validatorsPrefix      = []byte("validators")
	currentPrefix         = []byte("current")
	pendingPrefix         = []byte("pending")
	validatorPrefix       = []byte("validator")
	delegatorPrefix       = []byte("delegator")
	subnetValidatorPrefix = []byte("subnetValidator")
	blockPrefix           = []byte("block")
	txPrefix              = []byte("tx")
	utxoPrefix            = []byte("utxo")
	addressPrefix         = []byte("address")
	subnetPrefix          = []byte("subnet")
	chainPrefix           = []byte("chain")
	singletonPrefix       = []byte("singleton")

	timestampKey2     = []byte("timestamp")
	currentSupplyKey2 = []byte("current supply")
	lastAcceptedKey   = []byte("last accepted")
	initializedKey    = []byte("initialized")

	errWrongNetworkID = errors.New("tx has wrong network ID")

	_ internalState = &internalStateImpl{}
)

const (
	// priority values are used as part of the keys in the pending/current
	// validator state to ensure they are sorted in the order that they should
	// be added/removed.
	lowPriority byte = iota
	mediumPriority
	topPriority

	blockCacheSize   = 2048
	txCacheSize      = 2048
	utxoCacheSize    = 2048
	addressCacheSize = 2048
	chainCacheSize   = 2048
	chainDBCacheSize = 2048
)

type internalState interface {
	mutableState
	uptime.State

	AddCurrentStaker(tx *Tx, potentialReward uint64)
	DeleteCurrentStaker(tx *Tx)

	AddPendingStaker(tx *Tx)
	DeletePendingStaker(tx *Tx)

	SetCurrentStakerChainState(currentStakerChainState)
	SetPendingStakerChainState(pendingStakerChainState)

	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)

	GetBlock(blockID ids.ID) (Block, error)
	AddBlock(block Block)

	Abort()
	Commit() error
	CommitBatch() (database.Batch, error)
	Close() error
}

/*
 * VMDB
 * |-. validators
 * | |-. current
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> potential reward
 * | | '-. subnetValidator
 * | |   '-. list
 * | |     '-- txID -> nil
 * | '-. pending
 * |   |-. validator
 * |   | '-. list
 * |   |   '-- txID -> nil
 * |   |-. delegator
 * |   | '-. list
 * |   |   '-- txID -> nil
 * |   '-. subnetValidator
 * |     '-. list
 * |       '-- txID -> nil
 * |-. blocks
 * | '-- blockID -> block bytes
 * |-. txs
 * | '-- txID -> tx bytes + tx status
 * |- utxos
 * | '-- utxoID -> utxo bytes
 * |-. addresses
 * | '-. address
 * |   '-. list
 * |     '-- utxoID -> nil
 * |-. subnets
 * | '-. list
 * |   '-- txID -> nil
 * |-. chains
 * | '-. subnetID
 * |   '-. list
 * |     '-- txID -> nil
 * '-. singletons
 *   |-- initializedKey -> nil
 *   |-- timestampKey -> timestamp
 *   |-- currentSupplyKey -> currentSupply
 *   '-- lastAcceptedKey -> lastAccepted
 */
type internalStateImpl struct {
	vm *VM

	baseDB *versiondb.Database

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
	currentValidatorDB           linkeddb.LinkedDB
	currentDelegatorBaseDB       database.Database
	currentDelegatorDB           linkeddb.LinkedDB
	currentSubnetValidatorBaseDB database.Database
	currentSubnetValidatorDB     linkeddb.LinkedDB
	pendingValidatorsDB          database.Database
	pendingValidatorBaseDB       database.Database
	pendingValidatorDB           linkeddb.LinkedDB
	pendingDelegatorBaseDB       database.Database
	pendingDelegatorDB           linkeddb.LinkedDB
	pendingSubnetValidatorBaseDB database.Database
	pendingSubnetValidatorDB     linkeddb.LinkedDB

	addedBlocks map[ids.ID]Block // map of blockID -> Block
	blockCache  cache.Cacher     // cache of blockID -> Block, if the entry is nil, it is not in the database
	blockDB     database.Database

	addedTxs map[ids.ID]*txStatusImpl // map of txID -> {*Tx, Status}
	txCache  cache.Cacher             // cache of txID -> {*Tx, Status} if the entry is nil, it is not in the database
	txDB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoCache     cache.Cacher          // cache of UTXOID -> *UTXO if the UTXO is nil then it does not exist in the database
	utxoDB        database.Database

	// addressCache cache.Cacher // cache of address -> linkedDB
	// addressDB    database.Database

	cachedSubnets []*Tx // nil if the subnets haven't been loaded
	addedSubnets  []*Tx
	subnetBaseDB  database.Database
	subnetDB      linkeddb.LinkedDB

	addedChains  map[ids.ID][]*Tx // maps subnetID -> the newly added chains to the subnet
	chainCache   cache.Cacher     // cache of subnetID -> the chains after all local modifications []*Tx
	chainDBCache cache.Cacher     // cache of subnetID -> linkedDB
	chainDB      database.Database

	originalTimestamp, timestamp         time.Time
	originalCurrentSupply, currentSupply uint64
	originalLastAccepted, lastAccepted   ids.ID
	singletonDB                          database.Database
}

type stateTx struct {
	Tx     []byte `serialize:"true"`
	Status Status `serialize:"true"`
}

type stateBlk struct {
	Blk    []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

func newInternalState(vm *VM, db database.Database, genesis []byte) (internalState, error) {
	baseDB := versiondb.New(db)

	validatorsDB := prefixdb.New(validatorsPrefix, baseDB)

	currentValidatorsDB := prefixdb.New(currentPrefix, validatorsDB)
	currentValidatorBaseDB := prefixdb.New(validatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(delegatorPrefix, currentValidatorsDB)
	currentSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(pendingPrefix, validatorsDB)
	pendingValidatorBaseDB := prefixdb.New(validatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(delegatorPrefix, pendingValidatorsDB)
	pendingSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, pendingValidatorsDB)

	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)
	is := &internalStateImpl{
		vm: vm,

		baseDB: baseDB,

		uptimes:        make(map[ids.ShortID]*currentValidatorState),
		updatedUptimes: make(map[ids.ShortID]struct{}),

		validatorsDB:                 validatorsDB,
		currentValidatorsDB:          currentValidatorsDB,
		currentValidatorBaseDB:       currentValidatorBaseDB,
		currentValidatorDB:           linkeddb.NewDefault(currentValidatorBaseDB),
		currentDelegatorBaseDB:       currentDelegatorBaseDB,
		currentDelegatorDB:           linkeddb.NewDefault(currentDelegatorBaseDB),
		currentSubnetValidatorBaseDB: currentSubnetValidatorBaseDB,
		currentSubnetValidatorDB:     linkeddb.NewDefault(currentSubnetValidatorBaseDB),
		pendingValidatorsDB:          pendingValidatorsDB,
		pendingValidatorBaseDB:       pendingValidatorBaseDB,
		pendingValidatorDB:           linkeddb.NewDefault(pendingValidatorBaseDB),
		pendingDelegatorBaseDB:       pendingDelegatorBaseDB,
		pendingDelegatorDB:           linkeddb.NewDefault(pendingDelegatorBaseDB),
		pendingSubnetValidatorBaseDB: pendingSubnetValidatorBaseDB,
		pendingSubnetValidatorDB:     linkeddb.NewDefault(pendingSubnetValidatorBaseDB),

		addedBlocks: make(map[ids.ID]Block),
		blockCache:  &cache.LRU{Size: blockCacheSize},
		blockDB:     prefixdb.New(blockPrefix, baseDB),

		addedTxs: make(map[ids.ID]*txStatusImpl),
		txCache:  &cache.LRU{Size: txCacheSize},
		txDB:     prefixdb.New(txPrefix, baseDB),

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoCache:     &cache.LRU{Size: utxoCacheSize},
		utxoDB:        prefixdb.New(utxoPrefix, baseDB),

		// addressCache: &cache.LRU{Size: addressCacheSize},
		// addressDB:    prefixdb.New(addressPrefix, baseDB),

		subnetBaseDB: subnetBaseDB,
		subnetDB:     linkeddb.NewDefault(subnetBaseDB),

		addedChains:  make(map[ids.ID][]*Tx),
		chainCache:   &cache.LRU{Size: chainCacheSize},
		chainDBCache: &cache.LRU{Size: chainDBCacheSize},
		chainDB:      prefixdb.New(chainPrefix, baseDB),

		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}

	shouldInit, err := is.shouldInit()
	if err != nil {
		// Drop any errors on close to return the first error
		_ = is.Close()

		return nil, fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := is.init(genesis); err != nil {
			// Drop any errors on close to return the first error
			_ = is.Close()

			return nil, fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
	}

	if err := is.load(); err != nil {
		// Drop any errors on close to return the first error
		_ = is.Close()

		return nil, fmt.Errorf(
			"failed to load the database state: %w",
			err,
		)
	}
	return is, nil
}

func (st *internalStateImpl) GetTimestamp() time.Time          { return st.timestamp }
func (st *internalStateImpl) SetTimestamp(timestamp time.Time) { st.timestamp = timestamp }

func (st *internalStateImpl) GetCurrentSupply() uint64              { return st.currentSupply }
func (st *internalStateImpl) SetCurrentSupply(currentSupply uint64) { st.currentSupply = currentSupply }

func (st *internalStateImpl) GetLastAccepted() ids.ID             { return st.lastAccepted }
func (st *internalStateImpl) SetLastAccepted(lastAccepted ids.ID) { st.lastAccepted = lastAccepted }

func (st *internalStateImpl) GetSubnets() ([]*Tx, error) {
	if st.cachedSubnets != nil {
		return st.cachedSubnets, nil
	}

	subnetDBIt := st.subnetDB.NewIterator()
	defer subnetDBIt.Release()

	txs := []*Tx(nil)
	for subnetDBIt.Next() {
		subnetIDBytes := subnetDBIt.Key()
		subnetID, err := ids.ToID(subnetIDBytes)
		if err != nil {
			return nil, err
		}
		subnetTx, _, err := st.GetTx(subnetID)
		if err != nil {
			return nil, err
		}
		txs = append(txs, subnetTx)
	}
	if err := subnetDBIt.Error(); err != nil {
		return nil, err
	}
	txs = append(txs, st.addedSubnets...)
	st.cachedSubnets = txs
	return txs, nil
}
func (st *internalStateImpl) AddSubnet(createSubnetTx *Tx) {
	st.addedSubnets = append(st.addedSubnets, createSubnetTx)
	if st.cachedSubnets != nil {
		st.cachedSubnets = append(st.cachedSubnets, createSubnetTx)
	}
}

func (st *internalStateImpl) GetChains(subnetID ids.ID) ([]*Tx, error) {
	if chainsIntf, cached := st.chainCache.Get(subnetID); cached {
		return chainsIntf.([]*Tx), nil
	}
	chainDB := st.getChainDB(subnetID)
	chainDBIt := chainDB.NewIterator()
	defer chainDBIt.Release()

	txs := []*Tx(nil)
	for chainDBIt.Next() {
		chainIDBytes := chainDBIt.Key()
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			return nil, err
		}
		chainTx, _, err := st.GetTx(chainID)
		if err != nil {
			return nil, err
		}
		txs = append(txs, chainTx)
	}
	if err := chainDBIt.Error(); err != nil {
		return nil, err
	}
	txs = append(txs, st.addedChains[subnetID]...)
	st.chainCache.Put(subnetID, txs)
	return txs, nil
}

func (st *internalStateImpl) AddChain(createChainTxIntf *Tx) {
	createChainTx := createChainTxIntf.UnsignedTx.(*UnsignedCreateChainTx)
	subnetID := createChainTx.SubnetID
	st.addedChains[subnetID] = append(st.addedChains[subnetID], createChainTxIntf)
	if chainsIntf, cached := st.chainCache.Get(subnetID); cached {
		chains := chainsIntf.([]*Tx)
		chains = append(chains, createChainTxIntf)
		st.chainCache.Put(subnetID, chains)
	}
}

func (st *internalStateImpl) getChainDB(subnetID ids.ID) linkeddb.LinkedDB {
	if chainDBIntf, cached := st.chainDBCache.Get(subnetID); cached {
		return chainDBIntf.(linkeddb.LinkedDB)
	}
	rawChainDB := prefixdb.New(subnetID[:], st.chainDB)
	chainDB := linkeddb.NewDefault(rawChainDB)
	st.chainDBCache.Put(subnetID, chainDB)
	return chainDB
}

func (st *internalStateImpl) GetTx(txID ids.ID) (*Tx, Status, error) {
	if tx, exists := st.addedTxs[txID]; exists {
		if tx == nil {
			return nil, Unknown, database.ErrNotFound
		}
		return tx.tx, tx.status, nil
	}
	if txIntf, cached := st.txCache.Get(txID); cached {
		if txIntf == nil {
			return nil, Unknown, database.ErrNotFound
		}
		tx := txIntf.(*txStatusImpl)
		return tx.tx, tx.status, nil
	}
	txBytes, err := st.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		st.txCache.Put(txID, nil)
		return nil, Unknown, database.ErrNotFound
	} else if err != nil {
		return nil, Unknown, err
	}

	stx := stateTx{}
	if _, err := GenesisCodec.Unmarshal(txBytes, &stx); err != nil {
		return nil, Unknown, err
	}

	tx := Tx{}
	if _, err := GenesisCodec.Unmarshal(stx.Tx, &tx); err != nil {
		return nil, Unknown, err
	}
	if err := tx.Sign(GenesisCodec, nil); err != nil {
		return nil, Unknown, err
	}

	ptx := &txStatusImpl{
		tx:     &tx,
		status: stx.Status,
	}

	st.txCache.Put(txID, ptx)
	return ptx.tx, ptx.status, nil
}

func (st *internalStateImpl) AddTx(tx *Tx, status Status) {
	st.addedTxs[tx.ID()] = &txStatusImpl{
		tx:     tx,
		status: status,
	}
}

func (st *internalStateImpl) GetUTXO(utxoID avax.UTXOID) (*avax.UTXO, error) {
	return st.getUTXO(utxoID.InputID())
}

func (st *internalStateImpl) getUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := st.modifiedUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	if utxoIntf, cached := st.utxoCache.Get(utxoID); cached {
		if utxoIntf == nil {
			return nil, database.ErrNotFound
		}
		return utxoIntf.(*avax.UTXO), nil
	}
	utxoBytes, err := st.utxoDB.Get(utxoID[:])
	if err == database.ErrNotFound {
		st.utxoCache.Put(utxoID, nil)
		return nil, database.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	utxo := &avax.UTXO{}
	if _, err := GenesisCodec.Unmarshal(utxoBytes, utxo); err != nil {
		return nil, err
	}
	st.utxoCache.Put(utxoID, utxo)
	return utxo, nil
}

func (st *internalStateImpl) AddUTXO(utxo *avax.UTXO) {
	st.modifiedUTXOs[utxo.InputID()] = utxo
}

func (st *internalStateImpl) DeleteUTXO(utxoID avax.UTXOID) {
	st.modifiedUTXOs[utxoID.InputID()] = nil
}

func (st *internalStateImpl) GetBlock(blockID ids.ID) (Block, error) {
	if blk, exists := st.addedBlocks[blockID]; exists {
		return blk, nil
	}
	if blkIntf, cached := st.blockCache.Get(blockID); cached {
		if blkIntf == nil {
			return nil, database.ErrNotFound
		}
		return blkIntf.(Block), nil
	}

	blkBytes, err := st.blockDB.Get(blockID[:])
	if err == database.ErrNotFound {
		st.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound
	} else if err != nil {
		return nil, err
	}

	blkStatus := stateBlk{}
	if _, err := GenesisCodec.Unmarshal(blkBytes, &blkStatus); err != nil {
		return nil, err
	}

	var blk Block
	if _, err := GenesisCodec.Unmarshal(blkStatus.Blk, &blk); err != nil {
		return nil, err
	}
	if err := blk.initialize(st.vm, blkStatus.Blk, blkStatus.Status, blk); err != nil {
		return nil, err
	}

	st.blockCache.Put(blockID, blk)
	return blk, nil
}

func (st *internalStateImpl) AddBlock(block Block) {
	st.addedBlocks[block.ID()] = block
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
		st.utxoDB.Close(),
		// st.addressDB.Close(),
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

	UpDuration      time.Duration `serialize:"true"` // In seconds
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

			if err := st.currentValidatorDB.Put(txID[:], vdrBytes); err != nil {
				return err
			}
			st.uptimes[tx.Validator.NodeID] = vdr
		case *UnsignedAddDelegatorTx:
			if err := database.PutUInt64(st.currentDelegatorDB, txID[:], potentialReward); err != nil {
				return err
			}
		case *UnsignedAddSubnetValidatorTx:
			if err := st.currentSubnetValidatorDB.Put(txID[:], nil); err != nil {
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
			db = st.currentValidatorDB
			delete(st.uptimes, tx.Validator.NodeID)
			delete(st.updatedUptimes, tx.Validator.NodeID)
		case *UnsignedAddDelegatorTx:
			db = st.currentDelegatorDB
		case *UnsignedAddSubnetValidatorTx:
			db = st.currentSubnetValidatorDB
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
			db = st.pendingValidatorDB
		case *UnsignedAddDelegatorTx:
			db = st.pendingDelegatorDB
		case *UnsignedAddSubnetValidatorTx:
			db = st.pendingSubnetValidatorDB
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
			db = st.pendingValidatorDB
		case *UnsignedAddDelegatorTx:
			db = st.pendingDelegatorDB
		case *UnsignedAddSubnetValidatorTx:
			db = st.pendingSubnetValidatorDB
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

		if err := st.currentValidatorDB.Put(uptime.txID[:], uptimeBytes); err != nil {
			return err
		}
	}
	return nil
}

func (st *internalStateImpl) writeBlocks() error {
	for blkID, blk := range st.addedBlocks {
		blkID := blkID

		sblk := stateBlk{
			Blk:    blk.Bytes(),
			Status: blk.Status(),
		}
		btxBytes, err := GenesisCodec.Marshal(codecVersion, &sblk)
		if err != nil {
			return err
		}

		delete(st.addedBlocks, blkID)
		st.blockCache.Put(blkID, blk)
		if err := st.blockDB.Put(blkID[:], btxBytes); err != nil {
			return err
		}
	}
	return nil
}

func (st *internalStateImpl) writeTXs() error {
	for txID, txStatus := range st.addedTxs {
		txID := txID

		stx := stateTx{
			Tx:     txStatus.tx.Bytes(),
			Status: txStatus.status,
		}
		txBytes, err := GenesisCodec.Marshal(codecVersion, &stx)
		if err != nil {
			return err
		}

		delete(st.addedTxs, txID)
		st.txCache.Put(txID, txStatus)
		if err := st.txDB.Put(txID[:], txBytes); err != nil {
			return err
		}
	}
	return nil
}

func (st *internalStateImpl) writeUTXOs() error {
	for utxoID, utxo := range st.modifiedUTXOs {
		utxoID := utxoID

		delete(st.modifiedUTXOs, utxoID)
		st.utxoCache.Put(utxoID, utxo)

		if utxo == nil {
			if err := st.utxoDB.Delete(utxoID[:]); err != nil {
				return err
			}
			continue
		}

		utxoBytes, err := GenesisCodec.Marshal(codecVersion, utxo)
		if err != nil {
			return err
		}

		if err := st.utxoDB.Put(utxoID[:], utxoBytes); err != nil {
			return err
		}
	}
	return nil
}

func (st *internalStateImpl) writeSubnets() error {
	for _, subnet := range st.addedSubnets {
		subnetID := subnet.ID()

		if err := st.subnetDB.Put(subnetID[:], nil); err != nil {
			return err
		}
	}
	st.addedSubnets = nil
	return nil
}

func (st *internalStateImpl) writeChains() error {
	for subnetID, chains := range st.addedChains {
		for _, chain := range chains {
			chainDB := st.getChainDB(subnetID)

			chainID := chain.ID()
			if err := chainDB.Put(chainID[:], nil); err != nil {
				return err
			}
		}
		delete(st.addedChains, subnetID)
	}
	return nil
}

func (st *internalStateImpl) writeSingletons() error {
	if !st.originalTimestamp.Equal(st.timestamp) {
		if err := database.PutTimestamp(st.singletonDB, timestampKey2, st.timestamp); err != nil {
			return err
		}
		st.originalTimestamp = st.timestamp
	}
	if st.originalCurrentSupply != st.currentSupply {
		if err := database.PutUInt64(st.singletonDB, currentSupplyKey2, st.currentSupply); err != nil {
			return err
		}
		st.originalCurrentSupply = st.currentSupply
	}
	if st.originalLastAccepted != st.lastAccepted {
		if err := database.PutID(st.singletonDB, lastAcceptedKey, st.lastAccepted); err != nil {
			return err
		}
		st.originalLastAccepted = st.lastAccepted
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

func (st *internalStateImpl) loadSingletons() error {
	timestamp, err := database.GetTimestamp(st.singletonDB, timestampKey2)
	if err != nil {
		return err
	}
	st.originalTimestamp = timestamp
	st.timestamp = timestamp

	currentSupply, err := database.GetUInt64(st.singletonDB, currentSupplyKey2)
	if err != nil {
		return err
	}
	st.originalCurrentSupply = currentSupply
	st.currentSupply = currentSupply

	lastAccepted, err := database.GetID(st.singletonDB, lastAcceptedKey)
	if err != nil {
		return err
	}
	st.originalLastAccepted = lastAccepted
	st.lastAccepted = lastAccepted

	return nil
}

func (st *internalStateImpl) loadCurrentValidators() error {
	cs := &currentStakerChainStateImpl{
		validatorsByNodeID: make(map[ids.ShortID]*currentValidatorImpl),
		validatorsByTxID:   make(map[ids.ID]*validatorReward),
	}

	validatorIt := st.currentValidatorDB.NewIterator()
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

	delegatorIt := st.currentDelegatorDB.NewIterator()
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

	subnetValidatorIt := st.currentSubnetValidatorDB.NewIterator()
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

	validatorIt := st.pendingValidatorDB.NewIterator()
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

	delegatorIt := st.pendingDelegatorDB.NewIterator()
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

	subnetValidatorIt := st.pendingSubnetValidatorDB.NewIterator()
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

func (st *internalStateImpl) shouldInit() (bool, error) {
	has, err := st.singletonDB.Has(initializedKey)
	return !has, err
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

		reward := Reward(
			stakeDuration,
			stakeAmount,
			currentSupply,
			st.vm.StakeMintingPeriod,
		)
		newCurrentSupply, err := safemath.Add64(currentSupply, reward)
		if err != nil {
			return err
		}

		st.AddCurrentStaker(vdrTx, reward)
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
