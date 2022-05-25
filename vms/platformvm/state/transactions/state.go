// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/metadata"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/prometheus/client_golang/prometheus"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ TxState = &state{}

	errStartTimeTooEarly = errors.New("start time is before the current chain time")
	errStartAfterEndTime = errors.New("start time is after the end time")

	validatorsPrefix      = []byte("validators")
	currentPrefix         = []byte("current")
	pendingPrefix         = []byte("pending")
	validatorPrefix       = []byte("validator")
	delegatorPrefix       = []byte("delegator")
	subnetValidatorPrefix = []byte("subnetValidator")
	validatorDiffsPrefix  = []byte("validatorDiffs")
	txPrefix              = []byte("tx")
	rewardUTXOsPrefix     = []byte("rewardUTXOs")
	utxoPrefix            = []byte("utxo")
	subnetPrefix          = []byte("subnet")
	chainPrefix           = []byte("chain")
)

const (
	// priority values are used as part of the keys in the pending/current
	// validator state to ensure they are sorted in the order that they should
	// be added/removed.
	lowPriority byte = iota
	mediumPriority
	topPriority

	validatorDiffsCacheSize = 2048
	txCacheSize             = 2048
	rewardUTXOsCacheSize    = 2048
	chainCacheSize          = 2048
	chainDBCacheSize        = 2048
)

type TxState interface {
	Content
	Management
}

// Mutable interface collects all methods updating
// transactions state upon blocks execution.
type Mutable interface {
	ValidatorState
	UTXOState

	GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error)
	AddRewardUTXO(txID ids.ID, utxo *avax.UTXO)
	GetSubnets() ([]*signed.Tx, error)
	AddSubnet(createSubnetTx *signed.Tx)
	GetChains(subnetID ids.ID) ([]*signed.Tx, error)
	AddChain(createChainTx *signed.Tx)
	GetTx(txID ids.ID) (*signed.Tx, status.Status, error)
	AddTx(tx *signed.Tx, status status.Status)
}

// Content interface collects all methods to query and mutate
// all transactions related state. Note this Content is a superset
// of Mutable
type Content interface {
	Mutable
	uptime.State
	avax.UTXOReader

	AddCurrentStaker(tx *signed.Tx, potentialReward uint64)
	DeleteCurrentStaker(tx *signed.Tx)
	AddPendingStaker(tx *signed.Tx)
	DeletePendingStaker(tx *signed.Tx)
	GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error)
	MaxStakeAmount(
		subnetID ids.ID,
		nodeID ids.NodeID,
		startTime time.Time,
		endTime time.Time,
	) (uint64, error)
}

// Management interface collects all methods used to initialize
// transaction db upon vm initialization, along with methods to
// persist updated state.
type Management interface {
	SyncGenesis(
		genesisUtxos []*avax.UTXO,
		genesisValidator []*signed.Tx,
		genesisChains []*signed.Tx,
	) error
	LoadTxs() error

	WriteTxs() error
	CloseTxs() error
}

type state struct {
	metadata.DataState

	cfg        *config.Config
	ctx        *snow.Context
	localStake prometheus.Gauge
	totalStake prometheus.Gauge
	rewards    reward.Calculator

	baseDB database.Database

	validatorState

	addedCurrentStakers   []*ValidatorReward
	deletedCurrentStakers []*signed.Tx
	addedPendingStakers   []*signed.Tx
	deletedPendingStakers []*signed.Tx
	uptimes               map[ids.NodeID]*currentValidatorState // nodeID -> uptimes
	updatedUptimes        map[ids.NodeID]struct{}               // nodeID -> nil

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

	validatorDiffsCache cache.Cacher // cache of heightWithSubnet -> map[ids.ShortID]*ValidatorWeightDiff
	validatorDiffsDB    database.Database

	addedTxs map[ids.ID]*TxAndStatus // map of txID -> {*transaction.Tx, Status}
	txCache  cache.Cacher            // cache of txID -> {*transaction.Tx, Status} if the entry is nil, it is not in the database
	txDB     database.Database

	addedRewardUTXOs map[ids.ID][]*avax.UTXO // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher            // cache of txID -> []*UTXO
	rewardUTXODB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     avax.UTXOState

	cachedSubnets []*signed.Tx // nil if the subnets haven't been loaded
	addedSubnets  []*signed.Tx
	subnetBaseDB  database.Database
	subnetDB      linkeddb.LinkedDB

	addedChains  map[ids.ID][]*signed.Tx // maps subnetID -> the newly added chains to the subnet
	chainCache   cache.Cacher            // cache of subnetID -> the chains after all local modifications []*transaction.Tx
	chainDBCache cache.Cacher            // cache of subnetID -> linkedDB
	chainDB      database.Database
}

type ValidatorWeightDiff struct {
	Decrease bool   `serialize:"true"`
	Amount   uint64 `serialize:"true"`
}

type TxAndStatus struct {
	Tx     *signed.Tx
	Status status.Status
}

type heightWithSubnet struct {
	Height   uint64 `serialize:"true"`
	SubnetID ids.ID `serialize:"true"`
}

type txBytesAndStatus struct {
	Tx     []byte        `serialize:"true"`
	Status status.Status `serialize:"true"`
}

func NewState(
	baseDB database.Database,
	metadata metadata.DataState,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
) TxState {
	validatorsDB := prefixdb.New(validatorsPrefix, baseDB)

	currentValidatorsDB := prefixdb.New(currentPrefix, validatorsDB)
	currentValidatorBaseDB := prefixdb.New(validatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(delegatorPrefix, currentValidatorsDB)
	currentSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(pendingPrefix, validatorsDB)
	pendingValidatorBaseDB := prefixdb.New(validatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(delegatorPrefix, pendingValidatorsDB)
	pendingSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, pendingValidatorsDB)

	validatorDiffsDB := prefixdb.New(validatorDiffsPrefix, validatorsDB)

	rewardUTXODB := prefixdb.New(rewardUTXOsPrefix, baseDB)
	utxoDB := prefixdb.New(utxoPrefix, baseDB)
	utxoState := avax.NewUTXOState(utxoDB, unsigned.GenCodec)
	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)

	return &state{
		cfg:        cfg,
		ctx:        ctx,
		localStake: localStake,
		totalStake: totalStake,
		rewards:    rewards,

		baseDB:         baseDB,
		DataState:      metadata,
		uptimes:        make(map[ids.NodeID]*currentValidatorState),
		updatedUptimes: make(map[ids.NodeID]struct{}),

		validatorsDB:                 validatorsDB,
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
		validatorDiffsDB:             validatorDiffsDB,
		validatorDiffsCache:          &cache.LRU{Size: validatorDiffsCacheSize},

		addedTxs: make(map[ids.ID]*TxAndStatus),
		txDB:     prefixdb.New(txPrefix, baseDB),
		txCache:  &cache.LRU{Size: txCacheSize},

		addedRewardUTXOs: make(map[ids.ID][]*avax.UTXO),
		rewardUTXODB:     rewardUTXODB,
		rewardUTXOsCache: &cache.LRU{Size: rewardUTXOsCacheSize},

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoDB:        utxoDB,
		utxoState:     utxoState,

		subnetBaseDB: subnetBaseDB,
		subnetDB:     linkeddb.NewDefault(subnetBaseDB),

		addedChains:  make(map[ids.ID][]*signed.Tx),
		chainDB:      prefixdb.New(chainPrefix, baseDB),
		chainCache:   &cache.LRU{Size: chainCacheSize},
		chainDBCache: &cache.LRU{Size: chainDBCacheSize},
	}
}

func NewMeteredTransactionsState(
	baseDB database.Database,
	metadata metadata.DataState,
	metrics prometheus.Registerer,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
) (TxState, error) {
	validatorsDB := prefixdb.New(validatorsPrefix, baseDB)

	currentValidatorsDB := prefixdb.New(currentPrefix, validatorsDB)
	currentValidatorBaseDB := prefixdb.New(validatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(delegatorPrefix, currentValidatorsDB)
	currentSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(pendingPrefix, validatorsDB)
	pendingValidatorBaseDB := prefixdb.New(validatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(delegatorPrefix, pendingValidatorsDB)
	pendingSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, pendingValidatorsDB)

	validatorDiffsDB := prefixdb.New(validatorDiffsPrefix, validatorsDB)

	validatorDiffsCache, err := metercacher.New(
		"validator_diffs_cache",
		metrics,
		&cache.LRU{Size: validatorDiffsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	txCache, err := metercacher.New(
		"tx_cache",
		metrics,
		&cache.LRU{Size: txCacheSize},
	)
	if err != nil {
		return nil, err
	}

	rewardUTXODB := prefixdb.New(rewardUTXOsPrefix, baseDB)
	rewardUTXOsCache, err := metercacher.New(
		"reward_utxos_cache",
		metrics,
		&cache.LRU{Size: rewardUTXOsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	utxoDB := prefixdb.New(utxoPrefix, baseDB)
	utxoState, err := avax.NewMeteredUTXOState(utxoDB, unsigned.GenCodec, metrics)
	if err != nil {
		return nil, err
	}

	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)

	chainCache, err := metercacher.New(
		"chain_cache",
		metrics,
		&cache.LRU{Size: chainCacheSize},
	)
	if err != nil {
		return nil, err
	}

	chainDBCache, err := metercacher.New(
		"chain_db_cache",
		metrics,
		&cache.LRU{Size: chainDBCacheSize},
	)

	return &state{
		cfg:        cfg,
		ctx:        ctx,
		localStake: localStake,
		totalStake: totalStake,
		rewards:    rewards,

		baseDB:    baseDB,
		DataState: metadata,

		uptimes:        make(map[ids.NodeID]*currentValidatorState),
		updatedUptimes: make(map[ids.NodeID]struct{}),

		validatorsDB:                 validatorsDB,
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
		validatorDiffsDB:             validatorDiffsDB,
		validatorDiffsCache:          validatorDiffsCache,

		addedTxs: make(map[ids.ID]*TxAndStatus),
		txDB:     prefixdb.New(txPrefix, baseDB),
		txCache:  txCache,

		addedRewardUTXOs: make(map[ids.ID][]*avax.UTXO),
		rewardUTXODB:     rewardUTXODB,
		rewardUTXOsCache: rewardUTXOsCache,

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoDB:        utxoDB,
		utxoState:     utxoState,

		subnetBaseDB: subnetBaseDB,
		subnetDB:     linkeddb.NewDefault(subnetBaseDB),

		addedChains:  make(map[ids.ID][]*signed.Tx),
		chainDB:      prefixdb.New(chainPrefix, baseDB),
		chainCache:   chainCache,
		chainDBCache: chainDBCache,
	}, err
}

func (s *state) GetSubnets() ([]*signed.Tx, error) {
	if s.cachedSubnets != nil {
		return s.cachedSubnets, nil
	}

	subnetDBIt := s.subnetDB.NewIterator()
	defer subnetDBIt.Release()

	txs := []*signed.Tx(nil)
	for subnetDBIt.Next() {
		subnetIDBytes := subnetDBIt.Key()
		subnetID, err := ids.ToID(subnetIDBytes)
		if err != nil {
			return nil, err
		}
		subnetTx, _, err := s.GetTx(subnetID)
		if err != nil {
			return nil, err
		}
		txs = append(txs, subnetTx)
	}
	if err := subnetDBIt.Error(); err != nil {
		return nil, err
	}
	txs = append(txs, s.addedSubnets...)
	s.cachedSubnets = txs
	return txs, nil
}

func (s *state) AddSubnet(createSubnetTx *signed.Tx) {
	s.addedSubnets = append(s.addedSubnets, createSubnetTx)
	if s.cachedSubnets != nil {
		s.cachedSubnets = append(s.cachedSubnets, createSubnetTx)
	}
}

func (s *state) GetChains(subnetID ids.ID) ([]*signed.Tx, error) {
	if chainsIntf, cached := s.chainCache.Get(subnetID); cached {
		return chainsIntf.([]*signed.Tx), nil
	}
	chainDB := s.getChainDB(subnetID)
	chainDBIt := chainDB.NewIterator()
	defer chainDBIt.Release()

	txs := []*signed.Tx(nil)
	for chainDBIt.Next() {
		chainIDBytes := chainDBIt.Key()
		chainID, err := ids.ToID(chainIDBytes)
		if err != nil {
			return nil, err
		}
		chainTx, _, err := s.GetTx(chainID)
		if err != nil {
			return nil, err
		}
		txs = append(txs, chainTx)
	}
	if err := chainDBIt.Error(); err != nil {
		return nil, err
	}
	txs = append(txs, s.addedChains[subnetID]...)
	s.chainCache.Put(subnetID, txs)
	return txs, nil
}

func (s *state) AddChain(createChainTxIntf *signed.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*unsigned.CreateChainTx)
	subnetID := createChainTx.SubnetID
	s.addedChains[subnetID] = append(s.addedChains[subnetID], createChainTxIntf)
	if chainsIntf, cached := s.chainCache.Get(subnetID); cached {
		chains := chainsIntf.([]*signed.Tx)
		chains = append(chains, createChainTxIntf)
		s.chainCache.Put(subnetID, chains)
	}
}

func (s *state) getChainDB(subnetID ids.ID) linkeddb.LinkedDB {
	if chainDBIntf, cached := s.chainDBCache.Get(subnetID); cached {
		return chainDBIntf.(linkeddb.LinkedDB)
	}
	rawChainDB := prefixdb.New(subnetID[:], s.chainDB)
	chainDB := linkeddb.NewDefault(rawChainDB)
	s.chainDBCache.Put(subnetID, chainDB)
	return chainDB
}

func (s *state) GetTx(txID ids.ID) (*signed.Tx, status.Status, error) {
	if tx, exists := s.addedTxs[txID]; exists {
		return tx.Tx, tx.Status, nil
	}
	if txIntf, cached := s.txCache.Get(txID); cached {
		if txIntf == nil {
			return nil, status.Unknown, database.ErrNotFound
		}
		tx := txIntf.(*TxAndStatus)
		return tx.Tx, tx.Status, nil
	}
	txBytes, err := s.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		s.txCache.Put(txID, nil)
		return nil, status.Unknown, database.ErrNotFound
	} else if err != nil {
		return nil, status.Unknown, err
	}

	stx := txBytesAndStatus{}
	if _, err := unsigned.GenCodec.Unmarshal(txBytes, &stx); err != nil {
		return nil, status.Unknown, err
	}

	tx := signed.Tx{}
	if _, err := unsigned.GenCodec.Unmarshal(stx.Tx, &tx); err != nil {
		return nil, status.Unknown, err
	}
	if err := tx.Sign(unsigned.GenCodec, nil); err != nil {
		return nil, status.Unknown, err
	}

	ptx := &TxAndStatus{
		Tx:     &tx,
		Status: stx.Status,
	}

	s.txCache.Put(txID, ptx)
	return ptx.Tx, ptx.Status, nil
}

func (s *state) AddTx(tx *signed.Tx, status status.Status) {
	s.addedTxs[tx.ID()] = &TxAndStatus{
		Tx:     tx,
		Status: status,
	}
}

func (s *state) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := s.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	if utxos, exists := s.rewardUTXOsCache.Get(txID); exists {
		return utxos.([]*avax.UTXO), nil
	}

	rawTxDB := prefixdb.New(txID[:], s.rewardUTXODB)
	txDB := linkeddb.NewDefault(rawTxDB)
	it := txDB.NewIterator()
	defer it.Release()

	utxos := []*avax.UTXO(nil)
	for it.Next() {
		utxo := &avax.UTXO{}
		if _, err := unsigned.Codec.Unmarshal(it.Value(), utxo); err != nil {
			return nil, err
		}
		utxos = append(utxos, utxo)
	}
	if err := it.Error(); err != nil {
		return nil, err
	}

	s.rewardUTXOsCache.Put(txID, utxos)
	return utxos, nil
}

func (s *state) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	s.addedRewardUTXOs[txID] = append(s.addedRewardUTXOs[txID], utxo)
}

func (s *state) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := s.modifiedUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	return s.utxoState.GetUTXO(utxoID)
}

func (s *state) UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	return s.utxoState.UTXOIDs(addr, start, limit)
}

func (s *state) AddUTXO(utxo *avax.UTXO) {
	s.modifiedUTXOs[utxo.InputID()] = utxo
}

func (s *state) DeleteUTXO(utxoID ids.ID) {
	s.modifiedUTXOs[utxoID] = nil
}

func (s *state) GetUptime(nodeID ids.NodeID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	uptime, exists := s.uptimes[nodeID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return uptime.UpDuration, uptime.lastUpdated, nil
}

func (s *state) SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) error {
	uptime, exists := s.uptimes[nodeID]
	if !exists {
		return database.ErrNotFound
	}
	uptime.UpDuration = upDuration
	uptime.lastUpdated = lastUpdated
	s.updatedUptimes[nodeID] = struct{}{}
	return nil
}

func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	currentValidator, err := s.CurrentStakerChainState().GetValidator(nodeID)
	if err != nil {
		return time.Time{}, err
	}

	unsignedVdrTx, _ := currentValidator.AddValidatorTx()
	return unsignedVdrTx.StartTime(), nil
}

func (s *state) AddCurrentStaker(tx *signed.Tx, potentialReward uint64) {
	s.addedCurrentStakers = append(s.addedCurrentStakers, &ValidatorReward{
		AddStakerTx:     tx,
		PotentialReward: potentialReward,
	})
}

func (s *state) DeleteCurrentStaker(tx *signed.Tx) {
	s.deletedCurrentStakers = append(s.deletedCurrentStakers, tx)
}

func (s *state) AddPendingStaker(tx *signed.Tx) {
	s.addedPendingStakers = append(s.addedPendingStakers, tx)
}

func (s *state) DeletePendingStaker(tx *signed.Tx) {
	s.deletedPendingStakers = append(s.deletedPendingStakers, tx)
}

func (s *state) GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	prefixStruct := heightWithSubnet{
		Height:   height,
		SubnetID: subnetID,
	}
	prefixBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, prefixStruct)
	if err != nil {
		return nil, err
	}
	prefixStr := string(prefixBytes)

	if weightDiffsIntf, ok := s.validatorDiffsCache.Get(prefixStr); ok {
		return weightDiffsIntf.(map[ids.NodeID]*ValidatorWeightDiff), nil
	}

	rawDiffDB := prefixdb.New(prefixBytes, s.validatorDiffsDB)
	diffDB := linkeddb.NewDefault(rawDiffDB)
	diffIter := diffDB.NewIterator()

	weightDiffs := make(map[ids.NodeID]*ValidatorWeightDiff)
	for diffIter.Next() {
		nodeID, err := ids.ToNodeID(diffIter.Key())
		if err != nil {
			return nil, err
		}

		weightDiff := ValidatorWeightDiff{}
		_, err = unsigned.GenCodec.Unmarshal(diffIter.Value(), &weightDiff)
		if err != nil {
			return nil, err
		}

		weightDiffs[nodeID] = &weightDiff
	}

	s.validatorDiffsCache.Put(prefixStr, weightDiffs)
	return weightDiffs, nil
}

func (s *state) MaxStakeAmount(
	subnetID ids.ID,
	nodeID ids.NodeID,
	startTime time.Time,
	endTime time.Time,
) (uint64, error) {
	if startTime.After(endTime) {
		return 0, errStartAfterEndTime
	}
	if timestamp := s.GetTimestamp(); startTime.Before(timestamp) {
		return 0, errStartTimeTooEarly
	}
	if subnetID == constants.PrimaryNetworkID {
		return s.maxPrimarySubnetStakeAmount(nodeID, startTime, endTime)
	}
	return s.maxSubnetStakeAmount(subnetID, nodeID, startTime, endTime)
}

func (s *state) SyncGenesis(
	genesisUtxos []*avax.UTXO,
	genesisValidator []*signed.Tx,
	genesisChains []*signed.Tx,
) error {
	// Persist UTXOs that exist at genesis
	for _, utxo := range genesisUtxos {
		s.AddUTXO(utxo)
	}

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesisValidator {
		tx, ok := vdrTx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx but got %T", vdrTx.Unsigned)
		}

		stakeAmount := tx.Validator.Wght
		stakeDuration := tx.Validator.Duration()
		currentSupply := s.GetCurrentSupply()

		r := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := safemath.Add64(currentSupply, r)
		if err != nil {
			return err
		}

		s.AddCurrentStaker(vdrTx, r)
		s.AddTx(vdrTx, status.Committed)
		s.SetCurrentSupply(newCurrentSupply)
	}

	for _, chain := range genesisChains {
		unsignedChain, ok := chain.Unsigned.(*unsigned.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.CreateChainTx but got %T", chain.Unsigned)
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		if unsignedChain.NetworkID != s.ctx.NetworkID {
			return avax.ErrWrongNetworkID
		}

		s.AddChain(chain)
		s.AddTx(chain, status.Committed)
	}

	if err := s.WriteTxs(); err != nil {
		return err
	}

	return s.DataState.WriteMetadata()
}

func (s *state) LoadTxs() error {
	if err := s.LoadCurrentValidators(); err != nil {
		return err
	}
	return s.LoadPendingValidators()
}

func (s *state) LoadCurrentValidators() error {
	cs := &currentStakerState{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl),
		validatorsByTxID:   make(map[ids.ID]*ValidatorReward),
	}

	validatorIt := s.currentValidatorList.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		uptimeBytes := validatorIt.Value()
		uptime := &currentValidatorState{
			txID: txID,
		}
		if _, err := unsigned.Codec.Unmarshal(uptimeBytes, uptime); err != nil {
			return err
		}
		uptime.lastUpdated = time.Unix(int64(uptime.LastUpdated), 0)

		addValidatorTx, ok := tx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		cs.validatorsByNodeID[addValidatorTx.Validator.NodeID] = &currentValidatorImpl{
			validatorImpl: validatorImpl{
				subnets: make(map[ids.ID]signed.SubnetValidatorAndID),
			},
			addValidator: signed.ValidatorAndID{
				UnsignedAddValidatorTx: addValidatorTx,
				TxID:                   txID,
			},
			potentialReward: uptime.PotentialReward,
		}
		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx:     tx,
			PotentialReward: uptime.PotentialReward,
		}

		s.uptimes[addValidatorTx.Validator.NodeID] = uptime
	}

	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := s.currentDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		potentialRewardBytes := delegatorIt.Value()
		potentialReward, err := database.ParseUInt64(potentialRewardBytes)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.Unsigned.(*unsigned.AddDelegatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddDelegatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addDelegatorTx.Validator.NodeID]
		if !exists {
			return unsigned.ErrDelegatorSubset
		}
		vdr.delegatorWeight += addDelegatorTx.Validator.Wght
		vdr.delegators = append(vdr.delegators, signed.DelegatorAndID{
			UnsignedAddDelegatorTx: addDelegatorTx,
			TxID:                   txID,
		})
		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx:     tx,
			PotentialReward: potentialReward,
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := s.currentSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.Unsigned.(*unsigned.AddSubnetValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddSubnetValidatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addSubnetValidatorTx.Validator.NodeID]
		if !exists {
			return unsigned.ErrDSValidatorSubset
		}
		vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = signed.SubnetValidatorAndID{
			UnsignedAddSubnetValidator: addSubnetValidatorTx,
			TxID:                       txID,
		}

		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx: tx,
		}
	}
	if err := subnetValidatorIt.Error(); err != nil {
		return err
	}

	for _, vdr := range cs.validatorsByNodeID {
		sortDelegatorsByRemoval(vdr.delegators)
	}
	sortValidatorsByRemoval(cs.validators)
	cs.SetNextStaker()

	s.SetCurrentStakerChainState(cs)
	return nil
}

func (s *state) LoadPendingValidators() error {
	ps := &pendingStakerState{
		validatorsByNodeID:      make(map[ids.NodeID]signed.ValidatorAndID),
		validatorExtrasByNodeID: make(map[ids.NodeID]*validatorImpl),
	}

	validatorIt := s.pendingValidatorList.NewIterator()
	defer validatorIt.Release()
	for validatorIt.Next() {
		txIDBytes := validatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addValidatorTx, ok := tx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		ps.validatorsByNodeID[addValidatorTx.Validator.NodeID] = signed.ValidatorAndID{
			UnsignedAddValidatorTx: addValidatorTx,
			TxID:                   txID,
		}
	}
	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := s.pendingDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.Unsigned.(*unsigned.AddDelegatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddDelegatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID]; exists {
			vdr.delegators = append(vdr.delegators, signed.DelegatorAndID{
				UnsignedAddDelegatorTx: addDelegatorTx,
				TxID:                   txID,
			})
		} else {
			ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID] = &validatorImpl{
				delegators: []signed.DelegatorAndID{
					{
						UnsignedAddDelegatorTx: addDelegatorTx,
						TxID:                   txID,
					},
				},
				subnets: make(map[ids.ID]signed.SubnetValidatorAndID),
			}
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := s.pendingSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.Unsigned.(*unsigned.AddSubnetValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *unsigned.AddSubnetValidatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID]; exists {
			vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = signed.SubnetValidatorAndID{
				UnsignedAddSubnetValidator: addSubnetValidatorTx,
				TxID:                       txID,
			}
		} else {
			ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]signed.SubnetValidatorAndID{
					addSubnetValidatorTx.Validator.Subnet: {
						UnsignedAddSubnetValidator: addSubnetValidatorTx,
						TxID:                       txID,
					},
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

	s.SetPendingStakerChainState(ps)
	return nil
}

func (s *state) WriteTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeCurrentStakers(),
		s.writePendingStakers(),
		s.writeUptimes(),
		s.writeTXs(),
		s.writeRewardUTXOs(),
		s.writeUTXOs(),
		s.writeSubnets(),
		s.writeChains(),
	)
	return errs.Err
}

func (s *state) CloseTxs() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.pendingSubnetValidatorBaseDB.Close(),
		s.pendingDelegatorBaseDB.Close(),
		s.pendingValidatorBaseDB.Close(),
		s.pendingValidatorsDB.Close(),
		s.currentSubnetValidatorBaseDB.Close(),
		s.currentDelegatorBaseDB.Close(),
		s.currentValidatorBaseDB.Close(),
		s.currentValidatorsDB.Close(),
		s.validatorsDB.Close(),
		s.txDB.Close(),
		s.rewardUTXODB.Close(),
		s.utxoDB.Close(),
		s.subnetBaseDB.Close(),
		s.chainDB.Close(),
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

func (s *state) writeCurrentStakers() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to write current stakers with: %w", err)
		}
	}()

	weightDiffs := make(map[ids.ID]map[ids.NodeID]*ValidatorWeightDiff) // subnetID -> nodeID -> weightDiff
	for _, currentStaker := range s.addedCurrentStakers {
		txID := currentStaker.AddStakerTx.ID()
		potentialReward := currentStaker.PotentialReward

		var (
			subnetID ids.ID
			nodeID   ids.NodeID
			weight   uint64
		)
		switch tx := currentStaker.AddStakerTx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			var vdrBytes []byte
			startTime := tx.StartTime()
			vdr := &currentValidatorState{
				txID:        txID,
				lastUpdated: startTime,

				UpDuration:      0,
				LastUpdated:     uint64(startTime.Unix()),
				PotentialReward: potentialReward,
			}

			vdrBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, vdr)
			if err != nil {
				return
			}

			if err = s.currentValidatorList.Put(txID[:], vdrBytes); err != nil {
				return
			}
			s.uptimes[tx.Validator.NodeID] = vdr

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			if err = database.PutUInt64(s.currentDelegatorList, txID[:], potentialReward); err != nil {
				return
			}

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			if err = s.currentSubnetValidatorList.Put(txID[:], nil); err != nil {
				return
			}

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx or *unsigned.AddSubnetValidatorTx but got %T", tx)
		}

		subnetDiffs, ok := weightDiffs[subnetID]
		if !ok {
			subnetDiffs = make(map[ids.NodeID]*ValidatorWeightDiff)
			weightDiffs[subnetID] = subnetDiffs
		}

		nodeDiff, ok := subnetDiffs[nodeID]
		if !ok {
			nodeDiff = &ValidatorWeightDiff{}
			subnetDiffs[nodeID] = nodeDiff
		}

		var newWeight uint64
		newWeight, err = safemath.Add64(nodeDiff.Amount, weight)
		if err != nil {
			return err
		}
		nodeDiff.Amount = newWeight
	}
	s.addedCurrentStakers = nil

	for _, tx := range s.deletedCurrentStakers {
		var (
			db       database.KeyValueDeleter
			subnetID ids.ID
			nodeID   ids.NodeID
			weight   uint64
		)
		switch tx := tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.currentValidatorList

			delete(s.uptimes, tx.Validator.NodeID)
			delete(s.updatedUptimes, tx.Validator.NodeID)

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddDelegatorTx:
			db = s.currentDelegatorList

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *unsigned.AddSubnetValidatorTx:
			db = s.currentSubnetValidatorList

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx or *unsigned.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err = db.Delete(txID[:]); err != nil {
			return
		}

		subnetDiffs, ok := weightDiffs[subnetID]
		if !ok {
			subnetDiffs = make(map[ids.NodeID]*ValidatorWeightDiff)
			weightDiffs[subnetID] = subnetDiffs
		}

		nodeDiff, ok := subnetDiffs[nodeID]
		if !ok {
			nodeDiff = &ValidatorWeightDiff{}
			subnetDiffs[nodeID] = nodeDiff
		}

		if nodeDiff.Decrease {
			var newWeight uint64
			newWeight, err = safemath.Add64(nodeDiff.Amount, weight)
			if err != nil {
				return
			}
			nodeDiff.Amount = newWeight
		} else {
			nodeDiff.Decrease = nodeDiff.Amount < weight
			nodeDiff.Amount = safemath.Diff64(nodeDiff.Amount, weight)
		}
	}
	s.deletedCurrentStakers = nil

	for subnetID, nodeUpdates := range weightDiffs {
		var prefixBytes []byte
		prefixStruct := heightWithSubnet{
			Height:   s.DataState.GetHeight(),
			SubnetID: subnetID,
		}
		prefixBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, prefixStruct)
		if err != nil {
			return
		}
		rawDiffDB := prefixdb.New(prefixBytes, s.validatorDiffsDB)
		diffDB := linkeddb.NewDefault(rawDiffDB)
		for nodeID, nodeDiff := range nodeUpdates {
			if nodeDiff.Amount == 0 {
				delete(nodeUpdates, nodeID)
				continue
			}

			if subnetID == constants.PrimaryNetworkID || s.cfg.WhitelistedSubnets.Contains(subnetID) {
				if nodeDiff.Decrease {
					err = s.cfg.Validators.RemoveWeight(subnetID, nodeID, nodeDiff.Amount)
				} else {
					err = s.cfg.Validators.AddWeight(subnetID, nodeID, nodeDiff.Amount)
				}
				if err != nil {
					return
				}
			}

			var nodeDiffBytes []byte
			nodeDiffBytes, err = unsigned.GenCodec.Marshal(unsigned.Version, nodeDiff)
			if err != nil {
				return err
			}

			// Copy so value passed into [Put] doesn't get overwritten next iteration
			nodeID := nodeID
			if err := diffDB.Put(nodeID[:], nodeDiffBytes); err != nil {
				return err
			}
		}
		s.validatorDiffsCache.Put(string(prefixBytes), nodeUpdates)
	}

	// Attempt to update the stake metrics
	primaryValidators, ok := s.cfg.Validators.GetValidators(constants.PrimaryNetworkID)
	if !ok {
		return nil
	}
	weight, _ := primaryValidators.GetWeight(s.ctx.NodeID)
	s.localStake.Set(float64(weight))
	s.totalStake.Set(float64(primaryValidators.Weight()))
	return nil
}

func (s *state) writePendingStakers() error {
	for _, tx := range s.addedPendingStakers {
		var db database.KeyValueWriter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx or *unsigned.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err := db.Put(txID[:], nil); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	s.addedPendingStakers = nil

	for _, tx := range s.deletedPendingStakers {
		var db database.KeyValueDeleter
		switch tx.Unsigned.(type) {
		case *unsigned.AddValidatorTx:
			db = s.pendingValidatorList
		case *unsigned.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *unsigned.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return fmt.Errorf("expected tx type *unsigned.AddValidatorTx, *unsigned.AddDelegatorTx or *unsigned.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return fmt.Errorf("failed to write pending stakers with: %w", err)
		}
	}
	s.deletedPendingStakers = nil
	return nil
}

func (s *state) writeUptimes() error {
	for nodeID := range s.updatedUptimes {
		delete(s.updatedUptimes, nodeID)

		uptime := s.uptimes[nodeID]
		uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

		uptimeBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, uptime)
		if err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}

		if err := s.currentValidatorList.Put(uptime.txID[:], uptimeBytes); err != nil {
			return fmt.Errorf("failed to write uptimes with: %w", err)
		}
	}
	return nil
}

func (s *state) writeTXs() error {
	for txID, txStatus := range s.addedTxs {
		txID := txID

		stx := txBytesAndStatus{
			Tx:     txStatus.Tx.Bytes(),
			Status: txStatus.Status,
		}

		txBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, &stx)
		if err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}

		delete(s.addedTxs, txID)
		s.txCache.Put(txID, txStatus)
		if err := s.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to write txs with: %w", err)
		}
	}
	return nil
}

func (s *state) writeRewardUTXOs() error {
	for txID, utxos := range s.addedRewardUTXOs {
		delete(s.addedRewardUTXOs, txID)
		s.rewardUTXOsCache.Put(txID, utxos)
		rawTxDB := prefixdb.New(txID[:], s.rewardUTXODB)
		txDB := linkeddb.NewDefault(rawTxDB)

		for _, utxo := range utxos {
			utxoBytes, err := unsigned.GenCodec.Marshal(unsigned.Version, utxo)
			if err != nil {
				return fmt.Errorf("failed to write reward UTXOs with: %w", err)
			}
			utxoID := utxo.InputID()
			if err := txDB.Put(utxoID[:], utxoBytes); err != nil {
				return fmt.Errorf("failed to write reward UTXOs with: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writeUTXOs() error {
	for utxoID, utxo := range s.modifiedUTXOs {
		delete(s.modifiedUTXOs, utxoID)

		if utxo == nil {
			if err := s.utxoState.DeleteUTXO(utxoID); err != nil {
				return fmt.Errorf("failed to write UTXOs with: %w", err)
			}
			continue
		}
		if err := s.utxoState.PutUTXO(utxoID, utxo); err != nil {
			return fmt.Errorf("failed to write UTXOs with: %w", err)
		}
	}
	return nil
}

func (s *state) writeSubnets() error {
	for _, subnet := range s.addedSubnets {
		subnetID := subnet.ID()

		if err := s.subnetDB.Put(subnetID[:], nil); err != nil {
			return fmt.Errorf("failed to write current subnets with: %w", err)
		}
	}
	s.addedSubnets = nil
	return nil
}

func (s *state) writeChains() error {
	for subnetID, chains := range s.addedChains {
		for _, chain := range chains {
			chainDB := s.getChainDB(subnetID)

			chainID := chain.ID()
			if err := chainDB.Put(chainID[:], nil); err != nil {
				return fmt.Errorf("failed to write chains with: %w", err)
			}
		}
		delete(s.addedChains, subnetID)
	}
	return nil
}
