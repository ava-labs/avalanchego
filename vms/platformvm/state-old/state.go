// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	validatorDiffsCacheSize = 2048
	txCacheSize             = 2048
	rewardUTXOsCacheSize    = 2048
	chainCacheSize          = 2048
	chainDBCacheSize        = 2048
)

var (
	_ State = &state{}

	ErrDelegatorSubset = errors.New("delegator's time range must be a subset of the validator's time range")

	errDSValidatorSubset = errors.New("all subnets' staking period must be a subset of the primary network")
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
	singletonPrefix       = []byte("singleton")

	timestampKey     = []byte("timestamp")
	currentSupplyKey = []byte("current supply")
	lastAcceptedKey  = []byte("last accepted")
	initializedKey   = []byte("initialized")
)

// Chain collects all methods to manage the state of the chain for block
// execution.
type Chain interface {
	Stakers
	UTXOAdder
	UTXOGetter
	UTXODeleter

	GetTimestamp() time.Time
	SetTimestamp(tm time.Time)
	GetCurrentSupply() uint64
	SetCurrentSupply(cs uint64)

	GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error)
	AddRewardUTXO(txID ids.ID, utxo *avax.UTXO)
	GetSubnets() ([]*txs.Tx, error)
	AddSubnet(createSubnetTx *txs.Tx)
	GetChains(subnetID ids.ID) ([]*txs.Tx, error)
	AddChain(createChainTx *txs.Tx)
	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)
	AddTx(tx *txs.Tx, status status.Status)
}

type State interface {
	Chain
	uptime.State
	avax.UTXOReader

	// TODO: remove ShouldInit and DoneInit and perform them in New
	ShouldInit() (bool, error)
	DoneInit() error

	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)

	AddCurrentStaker(tx *txs.Tx, potentialReward uint64)
	DeleteCurrentStaker(tx *txs.Tx)
	AddPendingStaker(tx *txs.Tx)
	DeletePendingStaker(tx *txs.Tx)
	GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error)

	// Return the maximum amount of stake on a node (including delegations) at
	// any given time between [startTime] and [endTime] given that:
	// * The amount of stake on the node right now is [currentStake]
	// * The delegations currently on this node are [current]
	// * [current] is sorted in order of increasing delegation end time.
	// * The stake delegated in [current] are already included in [currentStake]
	// * [startTime] is in the future, and [endTime] > [startTime]
	// * The delegations that will be on this node in the future are [pending]
	// * The start time of all delegations in [pending] are in the future
	// * [pending] is sorted in order of increasing delegation start time
	MaxStakeAmount(
		subnetID ids.ID,
		nodeID ids.NodeID,
		startTime time.Time,
		endTime time.Time,
	) (uint64, error)

	// TODO: remove SyncGenesis and Load from the interface and perform them
	//       once upon creation

	// SyncGenesis initializes the state with the genesis state.
	SyncGenesis(genesisBlkID ids.ID, genesisState *genesis.State) error

	// Load pulls data previously stored on disk that is expected to be in
	// memory.
	Load() error

	Write(height uint64) error
	Close() error
}

type state struct {
	cfg        *config.Config
	ctx        *snow.Context
	localStake prometheus.Gauge
	totalStake prometheus.Gauge
	rewards    reward.Calculator

	baseDB database.Database

	stakers

	addedCurrentStakers   []*ValidatorReward
	deletedCurrentStakers []*txs.Tx
	addedPendingStakers   []*txs.Tx
	deletedPendingStakers []*txs.Tx
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

	addedTxs map[ids.ID]*txAndStatus // map of txID -> {*txs.Tx, Status}
	txCache  cache.Cacher            // cache of txID -> {*txs.Tx, Status} if the entry is nil, it is not in the database
	txDB     database.Database

	addedRewardUTXOs map[ids.ID][]*avax.UTXO // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher            // cache of txID -> []*UTXO
	rewardUTXODB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     avax.UTXOState

	cachedSubnets []*txs.Tx // nil if the subnets haven't been loaded
	addedSubnets  []*txs.Tx
	subnetBaseDB  database.Database
	subnetDB      linkeddb.LinkedDB

	addedChains  map[ids.ID][]*txs.Tx // maps subnetID -> the newly added chains to the subnet
	chainCache   cache.Cacher         // cache of subnetID -> the chains after all local modifications []*txs.Tx
	chainDBCache cache.Cacher         // cache of subnetID -> linkedDB
	chainDB      database.Database

	originalTimestamp, timestamp         time.Time
	originalCurrentSupply, currentSupply uint64
	originalLastAccepted, lastAccepted   ids.ID
	singletonDB                          database.Database
}

type ValidatorWeightDiff struct {
	Decrease bool   `serialize:"true"`
	Amount   uint64 `serialize:"true"`
}

type heightWithSubnet struct {
	Height   uint64 `serialize:"true"`
	SubnetID ids.ID `serialize:"true"`
}

type txBytesAndStatus struct {
	Tx     []byte        `serialize:"true"`
	Status status.Status `serialize:"true"`
}

func New(
	baseDB database.Database,
	metrics prometheus.Registerer,
	cfg *config.Config,
	ctx *snow.Context,
	localStake prometheus.Gauge,
	totalStake prometheus.Gauge,
	rewards reward.Calculator,
) (State, error) {
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
	utxoState, err := avax.NewMeteredUTXOState(utxoDB, genesis.Codec, metrics)
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

		baseDB: baseDB,

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

		addedTxs: make(map[ids.ID]*txAndStatus),
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

		addedChains:  make(map[ids.ID][]*txs.Tx),
		chainDB:      prefixdb.New(chainPrefix, baseDB),
		chainCache:   chainCache,
		chainDBCache: chainDBCache,

		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}, err
}

func (s *state) ShouldInit() (bool, error) {
	has, err := s.singletonDB.Has(initializedKey)
	return !has, err
}

func (s *state) DoneInit() error {
	return s.singletonDB.Put(initializedKey, nil)
}

func (s *state) GetSubnets() ([]*txs.Tx, error) {
	if s.cachedSubnets != nil {
		return s.cachedSubnets, nil
	}

	subnetDBIt := s.subnetDB.NewIterator()
	defer subnetDBIt.Release()

	txs := []*txs.Tx(nil)
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

func (s *state) AddSubnet(createSubnetTx *txs.Tx) {
	s.addedSubnets = append(s.addedSubnets, createSubnetTx)
	if s.cachedSubnets != nil {
		s.cachedSubnets = append(s.cachedSubnets, createSubnetTx)
	}
}

func (s *state) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	if chainsIntf, cached := s.chainCache.Get(subnetID); cached {
		return chainsIntf.([]*txs.Tx), nil
	}
	chainDB := s.getChainDB(subnetID)
	chainDBIt := chainDB.NewIterator()
	defer chainDBIt.Release()

	txs := []*txs.Tx(nil)
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

func (s *state) AddChain(createChainTxIntf *txs.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*txs.CreateChainTx)
	subnetID := createChainTx.SubnetID
	s.addedChains[subnetID] = append(s.addedChains[subnetID], createChainTxIntf)
	if chainsIntf, cached := s.chainCache.Get(subnetID); cached {
		chains := chainsIntf.([]*txs.Tx)
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

func (s *state) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := s.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}
	if txIntf, cached := s.txCache.Get(txID); cached {
		if txIntf == nil {
			return nil, status.Unknown, database.ErrNotFound
		}
		tx := txIntf.(*txAndStatus)
		return tx.tx, tx.status, nil
	}
	txBytes, err := s.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		s.txCache.Put(txID, nil)
		return nil, status.Unknown, database.ErrNotFound
	} else if err != nil {
		return nil, status.Unknown, err
	}

	stx := txBytesAndStatus{}
	if _, err := genesis.Codec.Unmarshal(txBytes, &stx); err != nil {
		return nil, status.Unknown, err
	}

	tx := txs.Tx{}
	if _, err := genesis.Codec.Unmarshal(stx.Tx, &tx); err != nil {
		return nil, status.Unknown, err
	}
	if err := tx.Sign(genesis.Codec, nil); err != nil {
		return nil, status.Unknown, err
	}

	ptx := &txAndStatus{
		tx:     &tx,
		status: stx.Status,
	}

	s.txCache.Put(txID, ptx)
	return ptx.tx, ptx.status, nil
}

func (s *state) AddTx(tx *txs.Tx, status status.Status) {
	s.addedTxs[tx.ID()] = &txAndStatus{
		tx:     tx,
		status: status,
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
		if _, err := txs.Codec.Unmarshal(it.Value(), utxo); err != nil {
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

func (s *state) GetTimestamp() time.Time             { return s.timestamp }
func (s *state) SetTimestamp(tm time.Time)           { s.timestamp = tm }
func (s *state) GetCurrentSupply() uint64            { return s.currentSupply }
func (s *state) SetCurrentSupply(cs uint64)          { s.currentSupply = cs }
func (s *state) GetLastAccepted() ids.ID             { return s.lastAccepted }
func (s *state) SetLastAccepted(lastAccepted ids.ID) { s.lastAccepted = lastAccepted }

func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	currentValidator, err := s.CurrentStakers().GetValidator(nodeID)
	if err != nil {
		return time.Time{}, err
	}

	unsignedVdrTx, _ := currentValidator.AddValidatorTx()
	return unsignedVdrTx.StartTime(), nil
}

func (s *state) AddCurrentStaker(tx *txs.Tx, potentialReward uint64) {
	s.addedCurrentStakers = append(s.addedCurrentStakers, &ValidatorReward{
		AddStakerTx:     tx,
		PotentialReward: potentialReward,
	})
}

func (s *state) DeleteCurrentStaker(tx *txs.Tx) {
	s.deletedCurrentStakers = append(s.deletedCurrentStakers, tx)
}

func (s *state) AddPendingStaker(tx *txs.Tx) {
	s.addedPendingStakers = append(s.addedPendingStakers, tx)
}

func (s *state) DeletePendingStaker(tx *txs.Tx) {
	s.deletedPendingStakers = append(s.deletedPendingStakers, tx)
}

func (s *state) GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	prefixStruct := heightWithSubnet{
		Height:   height,
		SubnetID: subnetID,
	}
	prefixBytes, err := genesis.Codec.Marshal(txs.Version, prefixStruct)
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
	defer diffIter.Release()

	weightDiffs := make(map[ids.NodeID]*ValidatorWeightDiff)
	for diffIter.Next() {
		nodeID, err := ids.ToNodeID(diffIter.Key())
		if err != nil {
			return nil, err
		}

		weightDiff := ValidatorWeightDiff{}
		_, err = genesis.Codec.Unmarshal(diffIter.Value(), &weightDiff)
		if err != nil {
			return nil, err
		}

		weightDiffs[nodeID] = &weightDiff
	}

	s.validatorDiffsCache.Put(prefixStr, weightDiffs)
	return weightDiffs, diffIter.Error()
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

func (s *state) SyncGenesis(genesisBlkID ids.ID, genesis *genesis.State) error {
	s.SetLastAccepted(genesisBlkID)
	s.SetTimestamp(time.Unix(int64(genesis.Timestamp), 0))
	s.SetCurrentSupply(genesis.InitialSupply)

	// Persist UTXOs that exist at genesis
	for _, utxo := range genesis.UTXOs {
		s.AddUTXO(utxo)
	}

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesis.Validators {
		tx, ok := vdrTx.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddValidatorTx but got %T", vdrTx.Unsigned)
		}

		stakeAmount := tx.Validator.Wght
		stakeDuration := tx.Validator.Duration()
		currentSupply := s.GetCurrentSupply()

		r := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := math.Add64(currentSupply, r)
		if err != nil {
			return err
		}

		s.AddCurrentStaker(vdrTx, r)
		s.AddTx(vdrTx, status.Committed)
		s.SetCurrentSupply(newCurrentSupply)
	}

	for _, chain := range genesis.Chains {
		unsignedChain, ok := chain.Unsigned.(*txs.CreateChainTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.CreateChainTx but got %T", chain.Unsigned)
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		if unsignedChain.NetworkID != s.ctx.NetworkID {
			return avax.ErrWrongNetworkID
		}

		s.AddChain(chain)
		s.AddTx(chain, status.Committed)
	}
	return s.Write(0)
}

func (s *state) Load() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.loadMetadata(),
		s.loadCurrentValidators(),
		s.loadPendingValidators(),
	)
	return errs.Err
}

func (s *state) loadMetadata() error {
	timestamp, err := database.GetTimestamp(s.singletonDB, timestampKey)
	if err != nil {
		return err
	}
	s.originalTimestamp = timestamp
	s.SetTimestamp(timestamp)

	currentSupply, err := database.GetUInt64(s.singletonDB, currentSupplyKey)
	if err != nil {
		return err
	}
	s.originalCurrentSupply = currentSupply
	s.SetCurrentSupply(currentSupply)

	lastAccepted, err := database.GetID(s.singletonDB, lastAcceptedKey)
	if err != nil {
		return err
	}
	s.originalLastAccepted = lastAccepted
	s.lastAccepted = lastAccepted
	return nil
}

func (s *state) loadCurrentValidators() error {
	cs := &currentStakers{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidator),
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
		if _, err := txs.Codec.Unmarshal(uptimeBytes, uptime); err != nil {
			return err
		}
		uptime.lastUpdated = time.Unix(int64(uptime.LastUpdated), 0)

		addValidatorTx, ok := tx.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddValidatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		cs.validatorsByNodeID[addValidatorTx.Validator.NodeID] = &currentValidator{
			validatorModifications: validatorModifications{
				subnets: make(map[ids.ID]SubnetValidatorAndID),
			},
			addValidator: ValidatorAndID{
				Tx:   addValidatorTx,
				TxID: txID,
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

		addDelegatorTx, ok := tx.Unsigned.(*txs.AddDelegatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddDelegatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addDelegatorTx.Validator.NodeID]
		if !exists {
			return ErrDelegatorSubset
		}
		vdr.delegatorWeight += addDelegatorTx.Validator.Wght
		vdr.delegators = append(vdr.delegators, DelegatorAndID{
			Tx:   addDelegatorTx,
			TxID: txID,
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

		addSubnetValidatorTx, ok := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddSubnetValidatorTx but got %T", tx.Unsigned)
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addSubnetValidatorTx.Validator.NodeID]
		if !exists {
			return errDSValidatorSubset
		}
		vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = SubnetValidatorAndID{
			Tx:   addSubnetValidatorTx,
			TxID: txID,
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

	s.SetCurrentStakers(cs)
	return nil
}

func (s *state) loadPendingValidators() error {
	ps := &pendingStakers{
		validatorsByNodeID:      make(map[ids.NodeID]ValidatorAndID),
		validatorExtrasByNodeID: make(map[ids.NodeID]*validatorModifications),
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

		addValidatorTx, ok := tx.Unsigned.(*txs.AddValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddValidatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		ps.validatorsByNodeID[addValidatorTx.Validator.NodeID] = ValidatorAndID{
			Tx:   addValidatorTx,
			TxID: txID,
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

		addDelegatorTx, ok := tx.Unsigned.(*txs.AddDelegatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddDelegatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID]; exists {
			vdr.delegators = append(vdr.delegators, DelegatorAndID{
				Tx:   addDelegatorTx,
				TxID: txID,
			})
		} else {
			ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID] = &validatorModifications{
				delegators: []DelegatorAndID{
					{
						Tx:   addDelegatorTx,
						TxID: txID,
					},
				},
				subnets: make(map[ids.ID]SubnetValidatorAndID),
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

		addSubnetValidatorTx, ok := tx.Unsigned.(*txs.AddSubnetValidatorTx)
		if !ok {
			return fmt.Errorf("expected tx type *txs.AddSubnetValidatorTx but got %T", tx.Unsigned)
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID]; exists {
			vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = SubnetValidatorAndID{
				Tx:   addSubnetValidatorTx,
				TxID: txID,
			}
		} else {
			ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID] = &validatorModifications{
				subnets: map[ids.ID]SubnetValidatorAndID{
					addSubnetValidatorTx.Validator.Subnet: {
						Tx:   addSubnetValidatorTx,
						TxID: txID,
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

	s.SetPendingStakers(ps)
	return nil
}

func (s *state) Write(height uint64) error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeCurrentStakers(height),
		s.writePendingStakers(),
		s.writeUptimes(),
		s.writeTXs(),
		s.writeRewardUTXOs(),
		s.writeUTXOs(),
		s.writeSubnets(),
		s.writeChains(),
		s.writeMetadata(),
	)
	return errs.Err
}

func (s *state) Close() error {
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
		s.singletonDB.Close(),
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

func (s *state) writeCurrentStakers(height uint64) error {
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
		case *txs.AddValidatorTx:
			startTime := tx.StartTime()
			vdr := &currentValidatorState{
				txID:        txID,
				lastUpdated: startTime,

				UpDuration:      0,
				LastUpdated:     uint64(startTime.Unix()),
				PotentialReward: potentialReward,
			}

			vdrBytes, err := genesis.Codec.Marshal(txs.Version, vdr)
			if err != nil {
				return fmt.Errorf("failed to serialize current validator: %w", err)
			}

			if err = s.currentValidatorList.Put(txID[:], vdrBytes); err != nil {
				return fmt.Errorf("failed to write current validator to list: %w", err)
			}
			s.uptimes[tx.Validator.NodeID] = vdr

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *txs.AddDelegatorTx:
			if err := database.PutUInt64(s.currentDelegatorList, txID[:], potentialReward); err != nil {
				return fmt.Errorf("failed to write current delegator to list: %w", err)
			}

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *txs.AddSubnetValidatorTx:
			if err := s.currentSubnetValidatorList.Put(txID[:], nil); err != nil {
				return fmt.Errorf("failed to write current subnet validator to list: %w", err)
			}

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return fmt.Errorf("expected tx type *txs.AddValidatorTx, *txs.AddDelegatorTx or *txs.AddSubnetValidatorTx but got %T", tx)
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

		newWeight, err := math.Add64(nodeDiff.Amount, weight)
		if err != nil {
			return fmt.Errorf("failed to increase node weight diff: %w", err)
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
		case *txs.AddValidatorTx:
			db = s.currentValidatorList

			delete(s.uptimes, tx.Validator.NodeID)
			delete(s.updatedUptimes, tx.Validator.NodeID)

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *txs.AddDelegatorTx:
			db = s.currentDelegatorList

			subnetID = constants.PrimaryNetworkID
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		case *txs.AddSubnetValidatorTx:
			db = s.currentSubnetValidatorList

			subnetID = tx.Validator.Subnet
			nodeID = tx.Validator.NodeID
			weight = tx.Validator.Wght
		default:
			return fmt.Errorf("expected tx type *txs.AddValidatorTx, *txs.AddDelegatorTx or *txs.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return fmt.Errorf("failed to delete current staker: %w", err)
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
			newWeight, err := math.Add64(nodeDiff.Amount, weight)
			if err != nil {
				return fmt.Errorf("failed to decrease node weight diff: %w", err)
			}
			nodeDiff.Amount = newWeight
		} else {
			nodeDiff.Decrease = nodeDiff.Amount < weight
			nodeDiff.Amount = math.Diff64(nodeDiff.Amount, weight)
		}
	}
	s.deletedCurrentStakers = nil

	for subnetID, nodeUpdates := range weightDiffs {
		prefixStruct := heightWithSubnet{
			Height:   height,
			SubnetID: subnetID,
		}
		prefixBytes, err := genesis.Codec.Marshal(txs.Version, prefixStruct)
		if err != nil {
			return fmt.Errorf("failed to create prefix bytes: %w", err)
		}
		rawDiffDB := prefixdb.New(prefixBytes, s.validatorDiffsDB)
		diffDB := linkeddb.NewDefault(rawDiffDB)
		for nodeID, nodeDiff := range nodeUpdates {
			if nodeDiff.Amount == 0 {
				delete(nodeUpdates, nodeID)
				continue
			}

			// TODO: Move the validator set management out of the state package
			if subnetID == constants.PrimaryNetworkID || s.cfg.WhitelistedSubnets.Contains(subnetID) {
				if nodeDiff.Decrease {
					err = s.cfg.Validators.RemoveWeight(subnetID, nodeID, nodeDiff.Amount)
				} else {
					err = s.cfg.Validators.AddWeight(subnetID, nodeID, nodeDiff.Amount)
				}
				if err != nil {
					return fmt.Errorf("failed to update validator weight: %w", err)
				}
			}

			nodeDiffBytes, err := genesis.Codec.Marshal(txs.Version, nodeDiff)
			if err != nil {
				return fmt.Errorf("failed to serialize validator weight diff: %w", err)
			}

			// Copy so value passed into [Put] doesn't get overwritten next
			// iteration
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
		case *txs.AddValidatorTx:
			db = s.pendingValidatorList
		case *txs.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *txs.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return fmt.Errorf("expected tx type *txs.AddValidatorTx, *txs.AddDelegatorTx or *txs.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err := db.Put(txID[:], nil); err != nil {
			return fmt.Errorf("failed to add pending staker: %w", err)
		}
	}
	s.addedPendingStakers = nil

	for _, tx := range s.deletedPendingStakers {
		var db database.KeyValueDeleter
		switch tx.Unsigned.(type) {
		case *txs.AddValidatorTx:
			db = s.pendingValidatorList
		case *txs.AddDelegatorTx:
			db = s.pendingDelegatorList
		case *txs.AddSubnetValidatorTx:
			db = s.pendingSubnetValidatorList
		default:
			return fmt.Errorf("expected tx type *txs.AddValidatorTx, *txs.AddDelegatorTx or *txs.AddSubnetValidatorTx but got %T", tx)
		}

		txID := tx.ID()
		if err := db.Delete(txID[:]); err != nil {
			return fmt.Errorf("failed to delete pending staker: %w", err)
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

		uptimeBytes, err := genesis.Codec.Marshal(txs.Version, uptime)
		if err != nil {
			return fmt.Errorf("failed to serialize uptime: %w", err)
		}

		if err := s.currentValidatorList.Put(uptime.txID[:], uptimeBytes); err != nil {
			return fmt.Errorf("failed to write uptime: %w", err)
		}
	}
	return nil
}

func (s *state) writeTXs() error {
	for txID, txStatus := range s.addedTxs {
		txID := txID

		stx := txBytesAndStatus{
			Tx:     txStatus.tx.Bytes(),
			Status: txStatus.status,
		}

		txBytes, err := genesis.Codec.Marshal(txs.Version, &stx)
		if err != nil {
			return fmt.Errorf("failed to serialize tx: %w", err)
		}

		delete(s.addedTxs, txID)
		s.txCache.Put(txID, txStatus)
		if err := s.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to add tx: %w", err)
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
			utxoBytes, err := genesis.Codec.Marshal(txs.Version, utxo)
			if err != nil {
				return fmt.Errorf("failed to serialize reward UTXO: %w", err)
			}
			utxoID := utxo.InputID()
			if err := txDB.Put(utxoID[:], utxoBytes); err != nil {
				return fmt.Errorf("failed to add reward UTXO: %w", err)
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
				return fmt.Errorf("failed to delete UTXO: %w", err)
			}
			continue
		}
		if err := s.utxoState.PutUTXO(utxoID, utxo); err != nil {
			return fmt.Errorf("failed to add UTXO: %w", err)
		}
	}
	return nil
}

func (s *state) writeSubnets() error {
	for _, subnet := range s.addedSubnets {
		subnetID := subnet.ID()

		if err := s.subnetDB.Put(subnetID[:], nil); err != nil {
			return fmt.Errorf("failed to write subnet: %w", err)
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
				return fmt.Errorf("failed to write chain: %w", err)
			}
		}
		delete(s.addedChains, subnetID)
	}
	return nil
}

func (s *state) writeMetadata() error {
	if !s.originalTimestamp.Equal(s.timestamp) {
		if err := database.PutTimestamp(s.singletonDB, timestampKey, s.timestamp); err != nil {
			return fmt.Errorf("failed to write timestamp: %w", err)
		}
		s.originalTimestamp = s.timestamp
	}
	if s.originalCurrentSupply != s.currentSupply {
		if err := database.PutUInt64(s.singletonDB, currentSupplyKey, s.currentSupply); err != nil {
			return fmt.Errorf("failed to write current supply: %w", err)
		}
		s.originalCurrentSupply = s.currentSupply
	}
	if s.originalLastAccepted != s.lastAccepted {
		if err := database.PutID(s.singletonDB, lastAcceptedKey, s.lastAccepted); err != nil {
			return fmt.Errorf("failed to write last accepted: %w", err)
		}
		s.originalLastAccepted = s.lastAccepted
	}
	return nil
}
