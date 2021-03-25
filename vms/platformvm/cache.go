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

	GetCurrentValidator(txID ids.ID) (addValidatorTx *Tx, potentialReward uint64)
	GetCurrentValidatorByNodeID(nodeID ids.ShortID) (addValidatorTx *Tx, potentialReward uint64)
	AddCurrentValidator(addValidatorTx *Tx, potentialReward uint64)
	DeleteCurrentValidator(txID ids.ID)

	GetCurrentDelegator(txID ids.ID) (addDelegatorTx *Tx, potentialReward uint64)
	GetCurrentDelegatorsByNodeID(nodeID ids.ShortID) []*Tx
	AddCurrentDelegator(addDelegatorTx *Tx, potentialReward uint64)
	DeleteCurrentDelegator(txID ids.ID)

	GetPendingValidator(txID ids.ID) *Tx
	GetPendingValidatorByNodeID(nodeID ids.ShortID) *Tx
	AddPendingValidator(*Tx)
	DeletePendingValidator(txID ids.ID)

	GetPendingDelegator(txID ids.ID) *Tx
	GetPendingDelegatorByNodeID(nodeID ids.ShortID) *Tx
	AddPendingDelegator(*Tx)
	DeletePendingDelegator(txID ids.ID)
}

type internalState interface {
	versionedState

	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)

	GetBlock(blockID ids.ID) (snowman.Block, error)
	AddBlock(block snowman.Block)

	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time)
	SetUptime(upDuration time.Duration, lastUpdated time.Time)

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

	timestampKey     = []byte("timestamp")
	currentSupplyKey = []byte("current supply")
	lastAcceptedKey  = []byte("last accepted")
)

const (
	blockCacheSize   = 2048
	txCacheSize      = 2048
	utxoCacheSize    = 2048
	addressCacheSize = 2048
	chainCacheSize   = 2048
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

	validatorBaseDB database.Database
	validatorDB     linkeddb.LinkedDB

	blockCache cache.Cacher
	blockDB    database.Database

	addedTxs map[ids.ID]*stateTx
	txCache  cache.Cacher
	txDB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO
	utxoCache     cache.Cacher
	utxoDB        database.Database

	addressCache cache.Cacher
	addressDB    database.Database

	subnetBaseDB database.Database
	subnetDB     linkeddb.LinkedDB

	addedChains map[ids.ID][]*Tx
	chainCache  cache.Cacher
	chainDB     database.Database

	originalTimestamp, timestamp         time.Time
	originalCurrentSupply, currentSupply uint64
	originalLastAccepted, lastAccepted   ids.ID
	singletonDB                          database.Database
}

type stateTx struct {
	Tx     *Tx    `serialize:"true"`
	Status Status `serialize:"true"`
}

func loadState(db database.Database) (internalState, error) {
	baseDB := versiondb.New(db)
	validatorBaseDB := prefixdb.New(validatorPrefix, baseDB)
	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)
	st := &internalStateImpl{
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

		addedChains: make(map[ids.ID][]*Tx),
		chainCache:  &cache.LRU{Size: chainCacheSize},
		chainDB:     prefixdb.New(chainPrefix, baseDB),

		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}

	timestamp, err := database.GetTimestamp(st.singletonDB, timestampKey)
	if err != nil {
		// Drop the close error to report the original error
		_ = st.Close()
		return nil, err
	}
	st.originalTimestamp = timestamp
	st.timestamp = timestamp

	currentSupply, err := database.GetUInt64(st.singletonDB, currentSupplyKey)
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

	return st
}

func (st *internalStateImpl) GetTimestamp() time.Time          { return st.timestamp }
func (st *internalStateImpl) SetTimestamp(timestamp time.Time) { st.timestamp = timestamp }

func (st *internalStateImpl) GetCurrentSupply() uint64              { return st.currentSupply }
func (st *internalStateImpl) SetCurrentSupply(currentSupply uint64) { st.currentSupply = currentSupply }

func (st *internalStateImpl) GetLastAccepted() ids.ID             { return st.lastAccepted }
func (st *internalStateImpl) SetLastAccepted(lastAccepted ids.ID) { st.lastAccepted = lastAccepted }

func (st *internalStateImpl) GetChains(subnetID ids.ID) ([]*Tx, error) {
}
func (st *internalStateImpl) AddChain(createChainTxIntf *Tx) {
	createChainTx := createChainTxIntf.UnsignedTx.(*UnsignedCreateChainTx)
	subnetID := createChainTx.SubnetID
	st.addedChains[subnetID] = append(st.addedChains[subnetID], createChainTxIntf)
	st.AddTx(createChainTxIntf, Committed)
}

// GetChains(subnetID ids.ID) ([]*Tx, error)
// AddChain(createChainTx *Tx)

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
