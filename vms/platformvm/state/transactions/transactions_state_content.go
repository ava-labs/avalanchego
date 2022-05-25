// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/state/metadata"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/prometheus/client_golang/prometheus"
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
