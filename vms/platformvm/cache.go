// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

type versionedState interface {
	GetTimestamp() time.Time
	SetTimestamp(time.Time)

	GetCurrentSupply() uint64
	SetCurrentSupply(uint64)

	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)

	GetSubnets() ([]*Tx, error)
	GetSubnet(subnetID ids.ID) (*Tx, error)
	AddSubnet(createSubnetTx *Tx)

	GetChains(subnetID ids.ID) ([]*Tx, error)
	GetChain(subnetID, chainID ids.ID) (*Tx, error)
	AddChain(createChainTx *Tx)

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

	GetBlock(blockID ids.ID) (snowman.Block, error)
	AddBlock(block snowman.Block)

	GetUptime(nodeID ids.ShortID) (upDuration time.Duration, lastUpdated time.Time, err error)
	SetUptime(upDuration time.Duration, lastUpdated time.Time)

	Commit() error
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
	baseDB      *versiondb.Database
	validatorDB linkeddb.LinkedDB
	blockDB     database.Database
	txDB        database.Database
	utxoDB      database.Database
	addressDB   database.Database
	subnetDB    linkeddb.LinkedDB
	chainDB     database.Database
	singletonDB database.Database

	originalTimestamp, timestamp time.Time
}

func loadState(db database.Database) (internalState, error) {
	baseDB := versiondb.New(db)
	st := &internalStateImpl{
		baseDB:      baseDB,
		validatorDB: linkeddb.NewDefault(prefixdb.New(validatorPrefix, baseDB)),
		blockDB:     prefixdb.New(blockPrefix, baseDB),
		txDB:        prefixdb.New(txPrefix, baseDB),
		utxoDB:      prefixdb.New(utxoPrefix, baseDB),
		addressDB:   prefixdb.New(addressPrefix, baseDB),
		subnetDB:    linkeddb.NewDefault(prefixdb.New(subnetPrefix, baseDB)),
		chainDB:     prefixdb.New(chainPrefix, baseDB),
		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}

	timestamp, err := st.loadTimestamp()
	if err != nil {
		return nil, err
	}
	st.originalTimestamp = timestamp
	st.timestamp = timestamp

	return st
}

func (st *internalStateImpl) loadTimestamp() (time.Time, error) {
	timestampBytes, err := st.singletonDB.Get(timestampKey)
	if err != nil {
		return time.Time{}, err
	}
	timestamp := time.Time{}
	if err := timestamp.UnmarshalBinary(timestampBytes); err != nil {
		return time.Time{}, err
	}
	return timestamp, nil
}
