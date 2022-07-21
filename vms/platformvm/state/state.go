// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/btree"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
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

	GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error)

	// Return the current validator set of [subnetID].
	ValidatorSet(subnetID ids.ID) (validators.Set, error)

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

	currentStakers *baseStakers
	pendingStakers *baseStakers

	uptimes        map[ids.NodeID]*uptimeAndReward // nodeID -> uptimes
	updatedUptimes map[ids.NodeID]struct{}         // nodeID -> nil

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

func (v *ValidatorWeightDiff) Add(negative bool, amount uint64) error {
	if v.Decrease == negative {
		var err error
		v.Amount, err = math.Add64(v.Amount, amount)
		return err
	}

	if v.Amount > amount {
		v.Amount -= amount
	} else {
		v.Amount = math.Diff64(v.Amount, amount)
		v.Decrease = negative
	}
	return nil
}

type heightWithSubnet struct {
	Height   uint64 `serialize:"true"`
	SubnetID ids.ID `serialize:"true"`
}

type txBytesAndStatus struct {
	Tx     []byte        `serialize:"true"`
	Status status.Status `serialize:"true"`
}

type txAndStatus struct {
	tx     *txs.Tx
	status status.Status
}

type uptimeAndReward struct {
	txID        ids.ID
	lastUpdated time.Time

	UpDuration      time.Duration `serialize:"true"`
	LastUpdated     uint64        `serialize:"true"` // Unix time in seconds
	PotentialReward uint64        `serialize:"true"`
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

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		uptimes:        make(map[ids.NodeID]*uptimeAndReward),
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

func (s *state) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.currentStakers.GetValidator(subnetID, nodeID)
}

func (s *state) PutCurrentValidator(staker *Staker) {
	s.currentStakers.PutValidator(staker)
}

func (s *state) DeleteCurrentValidator(staker *Staker) {
	s.currentStakers.DeleteValidator(staker)
}

func (s *state) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return s.currentStakers.GetDelegatorIterator(subnetID, nodeID), nil
}

func (s *state) PutCurrentDelegator(staker *Staker) {
	s.currentStakers.PutDelegator(staker)
}

func (s *state) DeleteCurrentDelegator(staker *Staker) {
	s.currentStakers.DeleteDelegator(staker)
}

func (s *state) GetCurrentStakerIterator() (StakerIterator, error) {
	return s.currentStakers.GetStakerIterator(), nil
}

func (s *state) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.pendingStakers.GetValidator(subnetID, nodeID)
}

func (s *state) PutPendingValidator(staker *Staker) {
	s.pendingStakers.PutValidator(staker)
}

func (s *state) DeletePendingValidator(staker *Staker) {
	s.pendingStakers.DeleteValidator(staker)
}

func (s *state) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return s.pendingStakers.GetDelegatorIterator(subnetID, nodeID), nil
}

func (s *state) PutPendingDelegator(staker *Staker) {
	s.pendingStakers.PutDelegator(staker)
}

func (s *state) DeletePendingDelegator(staker *Staker) {
	s.pendingStakers.DeleteDelegator(staker)
}

func (s *state) GetPendingStakerIterator() (StakerIterator, error) {
	return s.pendingStakers.GetStakerIterator(), nil
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

	tx, err := txs.Parse(genesis.Codec, stx.Tx)
	if err != nil {
		return nil, status.Unknown, err
	}

	ptx := &txAndStatus{
		tx:     tx,
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

// Returns the Primary Network start time of current validator [nodeID].
// Errors if [nodeID] isn't a current validator of the Primary Network.
func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	staker, err := s.currentStakers.GetValidator(constants.PrimaryNetworkID, nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return staker.StartTime, nil
}

func (s *state) GetTimestamp() time.Time             { return s.timestamp }
func (s *state) SetTimestamp(tm time.Time)           { s.timestamp = tm }
func (s *state) GetCurrentSupply() uint64            { return s.currentSupply }
func (s *state) SetCurrentSupply(cs uint64)          { s.currentSupply = cs }
func (s *state) GetLastAccepted() ids.ID             { return s.lastAccepted }
func (s *state) SetLastAccepted(lastAccepted ids.ID) { s.lastAccepted = lastAccepted }

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

func (s *state) ValidatorSet(subnetID ids.ID) (validators.Set, error) {
	vdrs := validators.NewSet()
	for nodeID, validator := range s.currentStakers.validators[subnetID] {
		staker := validator.validator
		if staker != nil {
			if err := vdrs.AddWeight(nodeID, staker.Weight); err != nil {
				return nil, err
			}
		}

		delegatorIterator := NewTreeIterator(validator.delegators)
		for delegatorIterator.Next() {
			staker := delegatorIterator.Value()
			if err := vdrs.AddWeight(nodeID, staker.Weight); err != nil {
				delegatorIterator.Release()
				return nil, err
			}
		}
		delegatorIterator.Release()
	}
	return vdrs, nil
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

		potentialReward := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := math.Add64(currentSupply, potentialReward)
		if err != nil {
			return err
		}

		staker := NewPrimaryNetworkStaker(vdrTx.ID(), &tx.Validator)
		staker.PotentialReward = potentialReward
		staker.NextTime = staker.EndTime
		staker.Priority = PrimaryNetworkValidatorCurrentPriority

		s.PutCurrentValidator(staker)
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
	s.currentStakers = newBaseStakers()

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
		uptime := &uptimeAndReward{
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

		staker := NewPrimaryNetworkStaker(txID, &addValidatorTx.Validator)
		staker.PotentialReward = uptime.PotentialReward
		staker.NextTime = staker.EndTime
		staker.Priority = PrimaryNetworkValidatorCurrentPriority

		validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.currentStakers.stakers.ReplaceOrInsert(staker)

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

		staker := NewPrimaryNetworkStaker(txID, &addDelegatorTx.Validator)
		staker.PotentialReward = potentialReward
		staker.NextTime = staker.EndTime
		staker.Priority = PrimaryNetworkDelegatorCurrentPriority

		validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		if validator.delegators == nil {
			validator.delegators = btree.New(defaultTreeDegree)
		}
		validator.delegators.ReplaceOrInsert(staker)

		s.currentStakers.stakers.ReplaceOrInsert(staker)
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

		staker := NewSubnetStaker(txID, &addSubnetValidatorTx.Validator)
		staker.NextTime = staker.EndTime
		staker.Priority = SubnetValidatorCurrentPriority

		validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.currentStakers.stakers.ReplaceOrInsert(staker)
	}
	return subnetValidatorIt.Error()
}

func (s *state) loadPendingValidators() error {
	s.pendingStakers = newBaseStakers()

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

		staker := NewPrimaryNetworkStaker(txID, &addValidatorTx.Validator)
		staker.NextTime = staker.StartTime
		staker.Priority = PrimaryNetworkValidatorPendingPriority

		validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.pendingStakers.stakers.ReplaceOrInsert(staker)
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

		staker := NewPrimaryNetworkStaker(txID, &addDelegatorTx.Validator)
		staker.NextTime = staker.StartTime
		staker.Priority = PrimaryNetworkDelegatorPendingPriority

		validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		if validator.delegators == nil {
			validator.delegators = btree.New(defaultTreeDegree)
		}
		validator.delegators.ReplaceOrInsert(staker)

		s.pendingStakers.stakers.ReplaceOrInsert(staker)
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

		staker := NewSubnetStaker(txID, &addSubnetValidatorTx.Validator)
		staker.NextTime = staker.StartTime
		staker.Priority = SubnetValidatorPendingPriority

		validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.pendingStakers.stakers.ReplaceOrInsert(staker)
	}
	return subnetValidatorIt.Error()
}

func (s *state) Write(height uint64) error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeCurrentPrimaryNetworkStakers(height),
		s.writeCurrentSubnetStakers(height),
		s.writePendingPrimaryNetworkStakers(),
		s.writePendingSubnetStakers(),
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

func (s *state) writeCurrentPrimaryNetworkStakers(height uint64) error {
	validatorDiffs, exists := s.currentStakers.validatorDiffs[constants.PrimaryNetworkID]
	if !exists {
		// If there are no validator changes, we shouldn't update any diffs.
		return nil
	}

	prefixStruct := heightWithSubnet{
		Height:   height,
		SubnetID: constants.PrimaryNetworkID,
	}
	prefixBytes, err := genesis.Codec.Marshal(txs.Version, prefixStruct)
	if err != nil {
		return fmt.Errorf("failed to create prefix bytes: %w", err)
	}
	rawDiffDB := prefixdb.New(prefixBytes, s.validatorDiffsDB)
	diffDB := linkeddb.NewDefault(rawDiffDB)

	weightDiffs := make(map[ids.NodeID]*ValidatorWeightDiff)
	for nodeID, validatorDiff := range validatorDiffs {
		weightDiff := &ValidatorWeightDiff{}
		if validatorDiff.validatorModified {
			staker := validatorDiff.validator

			weightDiff.Decrease = validatorDiff.validatorDeleted
			weightDiff.Amount = staker.Weight

			if validatorDiff.validatorDeleted {
				if err := s.currentValidatorList.Delete(staker.TxID[:]); err != nil {
					return fmt.Errorf("failed to delete current staker: %w", err)
				}

				delete(s.uptimes, nodeID)
				delete(s.updatedUptimes, nodeID)
			} else {
				vdr := &uptimeAndReward{
					txID:        staker.TxID,
					lastUpdated: staker.StartTime,

					UpDuration:      0,
					LastUpdated:     uint64(staker.StartTime.Unix()),
					PotentialReward: staker.PotentialReward,
				}

				vdrBytes, err := genesis.Codec.Marshal(txs.Version, vdr)
				if err != nil {
					return fmt.Errorf("failed to serialize current validator: %w", err)
				}

				if err = s.currentValidatorList.Put(staker.TxID[:], vdrBytes); err != nil {
					return fmt.Errorf("failed to write current validator to list: %w", err)
				}

				s.uptimes[nodeID] = vdr
			}
		}

		addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
		for addedDelegatorIterator.Next() {
			staker := addedDelegatorIterator.Value()

			if err := weightDiff.Add(false, staker.Weight); err != nil {
				addedDelegatorIterator.Release()
				return fmt.Errorf("failed to increase node weight diff: %w", err)
			}

			if err := database.PutUInt64(s.currentDelegatorList, staker.TxID[:], staker.PotentialReward); err != nil {
				addedDelegatorIterator.Release()
				return fmt.Errorf("failed to write current delegator to list: %w", err)
			}
		}
		addedDelegatorIterator.Release()

		for _, staker := range validatorDiff.deletedDelegators {
			if err := weightDiff.Add(true, staker.Weight); err != nil {
				return fmt.Errorf("failed to decrease node weight diff: %w", err)
			}

			if err := s.currentDelegatorList.Delete(staker.TxID[:]); err != nil {
				return fmt.Errorf("failed to delete current staker: %w", err)
			}
		}

		if weightDiff.Amount == 0 {
			continue
		}
		weightDiffs[nodeID] = weightDiff

		weightDiffBytes, err := genesis.Codec.Marshal(txs.Version, weightDiff)
		if err != nil {
			return fmt.Errorf("failed to serialize validator weight diff: %w", err)
		}

		// Copy so value passed into [Put] doesn't get overwritten next
		// iteration
		nodeID := nodeID
		if err := diffDB.Put(nodeID[:], weightDiffBytes); err != nil {
			return err
		}

		// TODO: Move the validator set management out of the state package
		if weightDiff.Decrease {
			err = s.cfg.Validators.RemoveWeight(constants.PrimaryNetworkID, nodeID, weightDiff.Amount)
		} else {
			err = s.cfg.Validators.AddWeight(constants.PrimaryNetworkID, nodeID, weightDiff.Amount)
		}
		if err != nil {
			return fmt.Errorf("failed to update validator weight: %w", err)
		}
	}
	s.validatorDiffsCache.Put(string(prefixBytes), weightDiffs)

	// TODO: Move validator set management out of the state package
	//
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

func (s *state) writeCurrentSubnetStakers(height uint64) error {
	for subnetID, subnetValidatorDiffs := range s.currentStakers.validatorDiffs {
		delete(s.currentStakers.validatorDiffs, subnetID)

		if subnetID == constants.PrimaryNetworkID {
			// It is assumed that this case is handled separately before calling
			// this function.
			continue
		}

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

		weightDiffs := make(map[ids.NodeID]*ValidatorWeightDiff)
		for nodeID, validatorDiff := range subnetValidatorDiffs {
			weightDiff := &ValidatorWeightDiff{}
			if validatorDiff.validatorModified {
				staker := validatorDiff.validator

				weightDiff.Decrease = validatorDiff.validatorDeleted
				weightDiff.Amount = staker.Weight

				if validatorDiff.validatorDeleted {
					err = s.currentSubnetValidatorList.Delete(staker.TxID[:])
				} else {
					err = s.currentSubnetValidatorList.Put(staker.TxID[:], nil)
				}
				if err != nil {
					return fmt.Errorf("failed to update current subnet staker: %w", err)
				}
			}

			// TODO: manage subnet delegators here

			if weightDiff.Amount == 0 {
				continue
			}
			weightDiffs[nodeID] = weightDiff

			weightDiffBytes, err := genesis.Codec.Marshal(txs.Version, weightDiff)
			if err != nil {
				return fmt.Errorf("failed to serialize validator weight diff: %w", err)
			}

			// Copy so value passed into [Put] doesn't get overwritten next
			// iteration
			nodeID := nodeID
			if err := diffDB.Put(nodeID[:], weightDiffBytes); err != nil {
				return err
			}

			// TODO: Move the validator set management out of the state package
			if s.cfg.WhitelistedSubnets.Contains(subnetID) {
				if weightDiff.Decrease {
					err = s.cfg.Validators.RemoveWeight(subnetID, nodeID, weightDiff.Amount)
				} else {
					err = s.cfg.Validators.AddWeight(subnetID, nodeID, weightDiff.Amount)
				}
				if err != nil {
					return fmt.Errorf("failed to update validator weight: %w", err)
				}
			}
		}
		s.validatorDiffsCache.Put(string(prefixBytes), weightDiffs)
	}
	return nil
}

func (s *state) writePendingPrimaryNetworkStakers() error {
	for _, validatorDiff := range s.pendingStakers.validatorDiffs[constants.PrimaryNetworkID] {
		if validatorDiff.validatorModified {
			staker := validatorDiff.validator

			var err error
			if validatorDiff.validatorDeleted {
				err = s.pendingValidatorList.Delete(staker.TxID[:])
			} else {
				err = s.pendingValidatorList.Put(staker.TxID[:], nil)
			}
			if err != nil {
				return fmt.Errorf("failed to update pending primary network staker: %w", err)
			}
		}

		addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
		for addedDelegatorIterator.Next() {
			staker := addedDelegatorIterator.Value()

			if err := s.pendingDelegatorList.Put(staker.TxID[:], nil); err != nil {
				addedDelegatorIterator.Release()
				return fmt.Errorf("failed to write pending delegator to list: %w", err)
			}
		}
		addedDelegatorIterator.Release()

		for _, staker := range validatorDiff.deletedDelegators {
			if err := s.pendingDelegatorList.Delete(staker.TxID[:]); err != nil {
				return fmt.Errorf("failed to delete pending delegator: %w", err)
			}
		}
	}
	return nil
}

func (s *state) writePendingSubnetStakers() error {
	for subnetID, subnetValidatorDiffs := range s.pendingStakers.validatorDiffs {
		delete(s.pendingStakers.validatorDiffs, subnetID)

		if subnetID == constants.PrimaryNetworkID {
			// It is assumed that this case is handled separately before calling
			// this function.
			continue
		}

		for _, validatorDiff := range subnetValidatorDiffs {
			if validatorDiff.validatorModified {
				staker := validatorDiff.validator

				var err error
				if validatorDiff.validatorDeleted {
					err = s.pendingSubnetValidatorList.Delete(staker.TxID[:])
				} else {
					err = s.pendingSubnetValidatorList.Put(staker.TxID[:], nil)
				}
				if err != nil {
					return fmt.Errorf("failed to update pending subnet staker: %w", err)
				}
			}

			// TODO: manage subnet delegators here
		}
	}
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
		if err := s.utxoState.PutUTXO(utxo); err != nil {
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
