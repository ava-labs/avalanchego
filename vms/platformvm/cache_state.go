// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type versionedState interface {
	immutableValidatorChainState

	GetTimestamp() time.Time
	SetTimestamp(time.Time)

	GetCurrentSupply() uint64
	SetCurrentSupply(uint64)

	GetSubnets() ([]*Tx, error)
	AddSubnet(createSubnetTx *Tx)

	GetChains(subnetID ids.ID) ([]*Tx, error)
	AddChain(createChainTx *Tx)

	GetTx(txID ids.ID) (*Tx, Status, error)
	AddTx(tx *Tx, status Status)

	GetUTXO(utxoID avax.UTXOID) (*avax.UTXO, error)
	AddUTXO(utxo *avax.UTXO)
	DeleteUTXO(utxoID avax.UTXOID)
}

type internalState interface {
	versionedState
	validatorState

	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)

	GetBlock(blockID ids.ID) (snowman.Block, error)
	AddBlock(block snowman.Block)

	Commit() error
	Close() error
}

var (
	validatorPrefix = []byte("validator")
	blockPrefix     = []byte("block")
	txPrefix        = []byte("tx")
	utxoPrefix      = []byte("utxo")
	addressPrefix   = []byte("address")
	subnetPrefix    = []byte("subnet")
	chainPrefix     = []byte("chain")
	singletonPrefix = []byte("singleton")

	timestampKey2     = []byte("timestamp")
	currentSupplyKey2 = []byte("current supply")
	lastAcceptedKey   = []byte("last accepted")
)

const (
	blockCacheSize   = 2048
	txCacheSize      = 2048
	utxoCacheSize    = 2048
	addressCacheSize = 2048
	chainCacheSize   = 2048
	chainDBCacheSize = 2048
)

/*
 * VMDB
 * |-. validators
 * | '-. list
 * |   '-- txID -> uptime + potential reward
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
 * | |-- timestampKey -> timestamp
 * | '-- currentSupplyKey -> currentSupply
 * | '-- lastAcceptedKey -> lastAccepted
 */
type internalStateImpl struct {
	baseDB *versiondb.Database

	currentValidators       map[ids.ID]*currentValidator // nodeID -> tx
	currentValidatorsByTxID map[ids.ID]ids.ID            // txID -> nodeID
	pendingValidators       map[ids.ID]*pendingValidator // nodeID -> tx
	pendingValidatorsByTxID map[ids.ID]ids.ID            // txID -> nodeID
	validatorBaseDB         database.Database
	validatorDB             linkeddb.LinkedDB

	blockCache cache.Cacher // cache of blockID -> *Block
	blockDB    database.Database

	addedTxs map[ids.ID]*stateTx // map of txID -> {*Tx, Status}
	txCache  cache.Cacher        // cache of txID -> {*Tx, Status} if the entry is nil, it is not in the database
	txDB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoCache     cache.Cacher          // cache of UTXOID -> *UTXO if the UTXO is nil then it does not exist in the database
	utxoDB        database.Database

	addressCache cache.Cacher // cache of address -> linkedDB
	addressDB    database.Database

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

type currentValidator struct {
	addValidatorTx    *Tx
	potentialReward   uint64
	upDuration        time.Duration
	lastUpdated       time.Time
	currentDelegators map[ids.ID]*currentDelegator // txID -> tx
	pendingDelegators map[ids.ID]*Tx               // txID -> tx
}

type currentDelegator struct {
	addDelegatorTx  *Tx
	potentialReward uint64
}

type pendingValidator struct {
	addValidatorTx    *Tx
	pendingDelegators map[ids.ID]*Tx // txID -> tx
}

type stateTx struct {
	Tx     *Tx    `serialize:"true"`
	Status Status `serialize:"true"`
}

func createInternalState(db database.Database) *internalStateImpl {
	baseDB := versiondb.New(db)
	validatorBaseDB := prefixdb.New(validatorPrefix, baseDB)
	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)
	return &internalStateImpl{
		baseDB: baseDB,

		validatorBaseDB: validatorBaseDB,
		validatorDB:     linkeddb.NewDefault(validatorBaseDB),

		blockCache: &cache.LRU{Size: blockCacheSize},
		blockDB:    prefixdb.New(blockPrefix, baseDB),

		addedTxs: make(map[ids.ID]*stateTx),
		txCache:  &cache.LRU{Size: txCacheSize},
		txDB:     prefixdb.New(txPrefix, baseDB),

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoCache:     &cache.LRU{Size: utxoCacheSize},
		utxoDB:        prefixdb.New(utxoPrefix, baseDB),

		addressCache: &cache.LRU{Size: addressCacheSize},
		addressDB:    prefixdb.New(addressPrefix, baseDB),

		subnetBaseDB: subnetBaseDB,
		subnetDB:     linkeddb.NewDefault(subnetBaseDB),

		addedChains:  make(map[ids.ID][]*Tx),
		chainCache:   &cache.LRU{Size: chainCacheSize},
		chainDBCache: &cache.LRU{Size: chainDBCacheSize},
		chainDB:      prefixdb.New(chainPrefix, baseDB),

		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}
}

func initState(db database.Database) (internalState, error) {
	st := createInternalState(db)
	return st, nil
}

func loadState(db database.Database) (internalState, error) {
	st := createInternalState(db)

	timestamp, err := database.GetTimestamp(st.singletonDB, timestampKey2)
	if err != nil {
		// Drop the close error to report the original error
		_ = st.Close()
		return nil, err
	}
	st.originalTimestamp = timestamp
	st.timestamp = timestamp

	currentSupply, err := database.GetUInt64(st.singletonDB, currentSupplyKey2)
	if err != nil {
		// Drop the close error to report the original error
		_ = st.Close()
		return nil, err
	}
	st.originalCurrentSupply = currentSupply
	st.currentSupply = currentSupply

	lastAccepted, err := database.GetID(st.singletonDB, lastAcceptedKey)
	if err != nil {
		// Drop the close error to report the original error
		_ = st.Close()
		return nil, err
	}
	st.originalLastAccepted = lastAccepted
	st.lastAccepted = lastAccepted

	validatorIt := st.validatorDB.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			// Drop the close error to report the original error
			_ = st.Close()
			return nil, err
		}
		tx, _, err := st.GetTx(txID)
		if err != nil {
			// Drop the close error to report the original error
			_ = st.Close()
			return nil, err
		}

		// TODO: do something with the tx
		_ = tx
	}
	if err := validatorIt.Error(); err != nil {
		// Drop the close error to report the original error
		_ = st.Close()
		return nil, err
	}

	return st, nil
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
	st.AddTx(createChainTxIntf, Committed)
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
		return tx.Tx, tx.Status, nil
	}
	if txIntf, cached := st.txCache.Get(txID); cached {
		tx := txIntf.(*stateTx)
		if tx == nil {
			return nil, Unknown, database.ErrNotFound
		}
		return tx.Tx, tx.Status, nil
	}
	txBytes, err := st.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		st.txCache.Put(txID, (*stateTx)(nil))
		return nil, Unknown, database.ErrNotFound
	} else if err != nil {
		return nil, Unknown, err
	}
	tx := &stateTx{}
	if _, err := GenesisCodec.Unmarshal(txBytes, tx); err != nil {
		return nil, Unknown, err
	}
	st.txCache.Put(txID, tx)
	return tx.Tx, tx.Status, nil
}
func (st *internalStateImpl) AddTx(tx *Tx, status Status) {
	st.addedTxs[tx.ID()] = &stateTx{
		Tx:     tx,
		Status: status,
	}
}

func (st *internalStateImpl) GetUTXO(utxoID avax.UTXOID) (*avax.UTXO, error) {
	utxoSourceID := utxoID.InputID()
	if utxo, exists := st.modifiedUTXOs[utxoSourceID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	if utxoIntf, cached := st.utxoCache.Get(utxoSourceID); cached {
		utxo := utxoIntf.(*avax.UTXO)
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	utxoBytes, err := st.utxoDB.Get(utxoSourceID[:])
	if err == database.ErrNotFound {
		st.utxoCache.Put(utxoSourceID, (*avax.UTXO)(nil))
		return nil, database.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	utxo := &avax.UTXO{}
	if _, err := GenesisCodec.Unmarshal(utxoBytes, utxo); err != nil {
		return nil, err
	}
	st.utxoCache.Put(utxoSourceID, utxo)
	return utxo, nil
}
func (st *internalStateImpl) AddUTXO(utxo *avax.UTXO) {
	st.modifiedUTXOs[utxo.InputID()] = utxo
}
func (st *internalStateImpl) DeleteUTXO(utxoID avax.UTXOID) {
	st.modifiedUTXOs[utxoID.InputID()] = nil
}

// TODO: Implement
func (st *internalStateImpl) GetBlock(blockID ids.ID) (snowman.Block, error) { return nil, nil }

// TODO: Implement
func (st *internalStateImpl) AddBlock(block snowman.Block) {}

// TODO: Implement
func (st *internalStateImpl) Commit() error { return nil }

func (st *internalStateImpl) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		st.validatorBaseDB.Close(),
		st.blockDB.Close(),
		st.txDB.Close(),
		st.utxoDB.Close(),
		st.addressDB.Close(),
		st.subnetBaseDB.Close(),
		st.chainDB.Close(),
		st.singletonDB.Close(),
		st.baseDB.Close(),
	)
	return errs.Err
}
