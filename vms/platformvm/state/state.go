// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const pointerOverhead = wrappers.LongLen

var (
	_ State = (*state)(nil)

	ErrDelegatorSubset              = errors.New("delegator's time range must be a subset of the validator's time range")
	ErrElasticSubnetConfigNotFound  = errors.New("elastic subnet configuration not found")
	errMissingValidatorSet          = errors.New("missing validator set")
	errValidatorSetAlreadyPopulated = errors.New("validator set already populated")
	errDuplicateValidatorSet        = errors.New("duplicate validator set")

	blockPrefix                   = []byte("block")
	validatorsPrefix              = []byte("validators")
	currentPrefix                 = []byte("current")
	pendingPrefix                 = []byte("pending")
	validatorPrefix               = []byte("validator")
	delegatorPrefix               = []byte("delegator")
	subnetValidatorPrefix         = []byte("subnetValidator")
	subnetDelegatorPrefix         = []byte("subnetDelegator")
	validatorWeightDiffsPrefix    = []byte("validatorDiffs")
	validatorPublicKeyDiffsPrefix = []byte("publicKeyDiffs")
	txPrefix                      = []byte("tx")
	rewardUTXOsPrefix             = []byte("rewardUTXOs")
	utxoPrefix                    = []byte("utxo")
	subnetPrefix                  = []byte("subnet")
	transformedSubnetPrefix       = []byte("transformedSubnet")
	supplyPrefix                  = []byte("supply")
	chainPrefix                   = []byte("chain")
	singletonPrefix               = []byte("singleton")

	timestampKey     = []byte("timestamp")
	currentSupplyKey = []byte("current supply")
	lastAcceptedKey  = []byte("last accepted")
	initializedKey   = []byte("initialized")
)

// Chain collects all methods to manage the state of the chain for block
// execution.
type Chain interface {
	Stakers
	avax.UTXOAdder
	avax.UTXOGetter
	avax.UTXODeleter

	GetTimestamp() time.Time
	SetTimestamp(tm time.Time)

	GetCurrentSupply(subnetID ids.ID) (uint64, error)
	SetCurrentSupply(subnetID ids.ID, cs uint64)

	GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error)
	AddRewardUTXO(txID ids.ID, utxo *avax.UTXO)

	GetSubnets() ([]*txs.Tx, error)
	AddSubnet(createSubnetTx *txs.Tx)

	GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error)
	AddSubnetTransformation(transformSubnetTx *txs.Tx)

	GetChains(subnetID ids.ID) ([]*txs.Tx, error)
	AddChain(createChainTx *txs.Tx)

	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)
	AddTx(tx *txs.Tx, status status.Status)

	GetRewardConfig(subnetID ids.ID) (reward.Config, error)
}

type State interface {
	Chain
	uptime.State
	avax.UTXOReader

	GetLastAccepted() ids.ID
	SetLastAccepted(blkID ids.ID)

	GetStatelessBlock(blockID ids.ID) (blocks.Block, error)

	// Invariant: [block] is an accepted block.
	AddStatelessBlock(block blocks.Block)

	// ValidatorSet adds all the validators and delegators of [subnetID] into
	// [vdrs].
	ValidatorSet(subnetID ids.ID, vdrs validators.Set) error

	GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error)

	// Returns a map of node ID --> BLS Public Key for all validators
	// that left the Primary Network validator set.
	GetValidatorPublicKeyDiffs(height uint64) (map[ids.NodeID]*bls.PublicKey, error)

	SetHeight(height uint64)

	// Discard uncommitted changes to the database.
	Abort()

	// Commit changes to the base database.
	Commit() error

	// Returns a batch of unwritten changes that, when written, will commit all
	// pending changes to the base database.
	CommitBatch() (database.Batch, error)

	Checksum() ids.ID

	Close() error
}

// TODO: Remove after v1.11.x is activated
type stateBlk struct {
	Blk    blocks.Block
	Bytes  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

/*
 * VMDB
 * |-. validators
 * | |-. current
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward + potential delegatee reward
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> potential reward
 * | | |-. subnetValidator
 * | | | '-. list
 * | | |   '-- txID -> uptime + potential reward + potential delegatee reward
 * | | '-. subnetDelegator
 * | |   '-. list
 * | |     '-- txID -> potential reward
 * | |-. pending
 * | | |-. validator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | |-. delegator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | |-. subnetValidator
 * | | | '-. list
 * | | |   '-- txID -> nil
 * | | '-. subnetDelegator
 * | |   '-. list
 * | |     '-- txID -> nil
 * | |-. weight diffs
 * | | '-. height+subnet
 * | |   '-. list
 * | |     '-- nodeID -> weightChange
 * | '-. pub key diffs
 * |   '-. height
 * |     '-. list
 * |       '-- nodeID -> public key
 * |-. blocks
 * | '-- blockID -> block bytes
 * |-. txs
 * | '-- txID -> tx bytes + tx status
 * |- rewardUTXOs
 * | '-. txID
 * |   '-. list
 * |     '-- utxoID -> utxo bytes
 * |- utxos
 * | '-- utxoDB
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
type state struct {
	validatorState

	cfg          *config.Config
	ctx          *snow.Context
	metrics      metrics.Metrics
	rewards      reward.Calculator
	bootstrapped *utils.Atomic[bool]

	baseDB *versiondb.Database

	currentStakers *baseStakers
	pendingStakers *baseStakers

	currentHeight uint64

	addedBlocks map[ids.ID]blocks.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, blocks.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

	validatorsDB                 database.Database
	currentValidatorsDB          database.Database
	currentValidatorBaseDB       database.Database
	currentValidatorList         linkeddb.LinkedDB
	currentDelegatorBaseDB       database.Database
	currentDelegatorList         linkeddb.LinkedDB
	currentSubnetValidatorBaseDB database.Database
	currentSubnetValidatorList   linkeddb.LinkedDB
	currentSubnetDelegatorBaseDB database.Database
	currentSubnetDelegatorList   linkeddb.LinkedDB
	pendingValidatorsDB          database.Database
	pendingValidatorBaseDB       database.Database
	pendingValidatorList         linkeddb.LinkedDB
	pendingDelegatorBaseDB       database.Database
	pendingDelegatorList         linkeddb.LinkedDB
	pendingSubnetValidatorBaseDB database.Database
	pendingSubnetValidatorList   linkeddb.LinkedDB
	pendingSubnetDelegatorBaseDB database.Database
	pendingSubnetDelegatorList   linkeddb.LinkedDB

	validatorWeightDiffsCache cache.Cacher[string, map[ids.NodeID]*ValidatorWeightDiff] // cache of heightWithSubnet -> map[ids.NodeID]*ValidatorWeightDiff
	validatorWeightDiffsDB    database.Database

	validatorPublicKeyDiffsCache cache.Cacher[uint64, map[ids.NodeID]*bls.PublicKey] // cache of height -> map[ids.NodeID]*bls.PublicKey
	validatorPublicKeyDiffsDB    database.Database

	addedTxs map[ids.ID]*txAndStatus            // map of txID -> {*txs.Tx, Status}
	txCache  cache.Cacher[ids.ID, *txAndStatus] // txID -> {*txs.Tx, Status}. If the entry is nil, it isn't in the database
	txDB     database.Database

	addedRewardUTXOs map[ids.ID][]*avax.UTXO            // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher[ids.ID, []*avax.UTXO] // txID -> []*UTXO
	rewardUTXODB     database.Database

	modifiedUTXOs map[ids.ID]*avax.UTXO // map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	utxoDB        database.Database
	utxoState     avax.UTXOState

	cachedSubnets []*txs.Tx // nil if the subnets haven't been loaded
	addedSubnets  []*txs.Tx
	subnetBaseDB  database.Database
	subnetDB      linkeddb.LinkedDB

	transformedSubnets     map[ids.ID]*txs.Tx            // map of subnetID -> transformSubnetTx
	transformedSubnetCache cache.Cacher[ids.ID, *txs.Tx] // cache of subnetID -> transformSubnetTx if the entry is nil, it is not in the database
	transformedSubnetDB    database.Database

	modifiedSupplies map[ids.ID]uint64             // map of subnetID -> current supply
	supplyCache      cache.Cacher[ids.ID, *uint64] // cache of subnetID -> current supply if the entry is nil, it is not in the database
	supplyDB         database.Database

	addedChains  map[ids.ID][]*txs.Tx                    // maps subnetID -> the newly added chains to the subnet
	chainCache   cache.Cacher[ids.ID, []*txs.Tx]         // cache of subnetID -> the chains after all local modifications []*txs.Tx
	chainDBCache cache.Cacher[ids.ID, linkeddb.LinkedDB] // cache of subnetID -> linkedDB
	chainDB      database.Database

	// The persisted fields represent the current database value
	timestamp, persistedTimestamp         time.Time
	currentSupply, persistedCurrentSupply uint64
	// [lastAccepted] is the most recently accepted block.
	lastAccepted, persistedLastAccepted ids.ID
	singletonDB                         database.Database
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
		v.Amount = math.AbsDiff(v.Amount, amount)
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

func txSize(tx *txs.Tx) int {
	if tx == nil {
		return pointerOverhead
	}
	return len(tx.Bytes()) + pointerOverhead
}

func txAndStatusSize(t *txAndStatus) int {
	if t == nil {
		return pointerOverhead
	}
	return len(t.tx.Bytes()) + wrappers.IntLen + pointerOverhead
}

func blockSize(blk blocks.Block) int {
	if blk == nil {
		return pointerOverhead
	}
	return len(blk.Bytes()) + pointerOverhead
}

func New(
	db database.Database,
	genesisBytes []byte,
	metricsReg prometheus.Registerer,
	cfg *config.Config,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metrics metrics.Metrics,
	rewards reward.Calculator,
	bootstrapped *utils.Atomic[bool],
) (State, error) {
	s, err := new(
		db,
		metrics,
		cfg,
		execCfg,
		ctx,
		metricsReg,
		rewards,
		bootstrapped,
	)
	if err != nil {
		return nil, err
	}

	if err := s.sync(genesisBytes); err != nil {
		// Drop any errors on close to return the first error
		_ = s.Close()

		return nil, err
	}

	return s, nil
}

func new(
	db database.Database,
	metrics metrics.Metrics,
	cfg *config.Config,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metricsReg prometheus.Registerer,
	rewards reward.Calculator,
	bootstrapped *utils.Atomic[bool],
) (*state, error) {
	blockCache, err := metercacher.New[ids.ID, blocks.Block](
		"block_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, blocks.Block](execCfg.BlockCacheSize, blockSize),
	)
	if err != nil {
		return nil, err
	}

	baseDB := versiondb.New(db)

	validatorsDB := prefixdb.New(validatorsPrefix, baseDB)

	currentValidatorsDB := prefixdb.New(currentPrefix, validatorsDB)
	currentValidatorBaseDB := prefixdb.New(validatorPrefix, currentValidatorsDB)
	currentDelegatorBaseDB := prefixdb.New(delegatorPrefix, currentValidatorsDB)
	currentSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, currentValidatorsDB)
	currentSubnetDelegatorBaseDB := prefixdb.New(subnetDelegatorPrefix, currentValidatorsDB)

	pendingValidatorsDB := prefixdb.New(pendingPrefix, validatorsDB)
	pendingValidatorBaseDB := prefixdb.New(validatorPrefix, pendingValidatorsDB)
	pendingDelegatorBaseDB := prefixdb.New(delegatorPrefix, pendingValidatorsDB)
	pendingSubnetValidatorBaseDB := prefixdb.New(subnetValidatorPrefix, pendingValidatorsDB)
	pendingSubnetDelegatorBaseDB := prefixdb.New(subnetDelegatorPrefix, pendingValidatorsDB)

	validatorWeightDiffsDB := prefixdb.New(validatorWeightDiffsPrefix, validatorsDB)
	validatorWeightDiffsCache, err := metercacher.New[string, map[ids.NodeID]*ValidatorWeightDiff](
		"validator_weight_diffs_cache",
		metricsReg,
		&cache.LRU[string, map[ids.NodeID]*ValidatorWeightDiff]{Size: execCfg.ValidatorDiffsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	validatorPublicKeyDiffsDB := prefixdb.New(validatorPublicKeyDiffsPrefix, validatorsDB)
	validatorPublicKeyDiffsCache, err := metercacher.New[uint64, map[ids.NodeID]*bls.PublicKey](
		"validator_pub_key_diffs_cache",
		metricsReg,
		&cache.LRU[uint64, map[ids.NodeID]*bls.PublicKey]{Size: execCfg.ValidatorDiffsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	txCache, err := metercacher.New(
		"tx_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txAndStatus](execCfg.TxCacheSize, txAndStatusSize),
	)
	if err != nil {
		return nil, err
	}

	rewardUTXODB := prefixdb.New(rewardUTXOsPrefix, baseDB)
	rewardUTXOsCache, err := metercacher.New[ids.ID, []*avax.UTXO](
		"reward_utxos_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*avax.UTXO]{Size: execCfg.RewardUTXOsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	utxoDB := prefixdb.New(utxoPrefix, baseDB)
	utxoState, err := avax.NewMeteredUTXOState(utxoDB, txs.GenesisCodec, metricsReg, execCfg.ChecksumsEnabled)
	if err != nil {
		return nil, err
	}

	subnetBaseDB := prefixdb.New(subnetPrefix, baseDB)

	transformedSubnetCache, err := metercacher.New(
		"transformed_subnet_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txs.Tx](execCfg.TransformedSubnetTxCacheSize, txSize),
	)
	if err != nil {
		return nil, err
	}

	supplyCache, err := metercacher.New[ids.ID, *uint64](
		"supply_cache",
		metricsReg,
		&cache.LRU[ids.ID, *uint64]{Size: execCfg.ChainCacheSize},
	)
	if err != nil {
		return nil, err
	}

	chainCache, err := metercacher.New[ids.ID, []*txs.Tx](
		"chain_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*txs.Tx]{Size: execCfg.ChainCacheSize},
	)
	if err != nil {
		return nil, err
	}

	chainDBCache, err := metercacher.New[ids.ID, linkeddb.LinkedDB](
		"chain_db_cache",
		metricsReg,
		&cache.LRU[ids.ID, linkeddb.LinkedDB]{Size: execCfg.ChainDBCacheSize},
	)
	if err != nil {
		return nil, err
	}

	return &state{
		validatorState: newValidatorState(),

		cfg:          cfg,
		ctx:          ctx,
		metrics:      metrics,
		rewards:      rewards,
		bootstrapped: bootstrapped,
		baseDB:       baseDB,

		addedBlocks: make(map[ids.ID]blocks.Block),
		blockCache:  blockCache,
		blockDB:     prefixdb.New(blockPrefix, baseDB),

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		validatorsDB:                 validatorsDB,
		currentValidatorsDB:          currentValidatorsDB,
		currentValidatorBaseDB:       currentValidatorBaseDB,
		currentValidatorList:         linkeddb.NewDefault(currentValidatorBaseDB),
		currentDelegatorBaseDB:       currentDelegatorBaseDB,
		currentDelegatorList:         linkeddb.NewDefault(currentDelegatorBaseDB),
		currentSubnetValidatorBaseDB: currentSubnetValidatorBaseDB,
		currentSubnetValidatorList:   linkeddb.NewDefault(currentSubnetValidatorBaseDB),
		currentSubnetDelegatorBaseDB: currentSubnetDelegatorBaseDB,
		currentSubnetDelegatorList:   linkeddb.NewDefault(currentSubnetDelegatorBaseDB),
		pendingValidatorsDB:          pendingValidatorsDB,
		pendingValidatorBaseDB:       pendingValidatorBaseDB,
		pendingValidatorList:         linkeddb.NewDefault(pendingValidatorBaseDB),
		pendingDelegatorBaseDB:       pendingDelegatorBaseDB,
		pendingDelegatorList:         linkeddb.NewDefault(pendingDelegatorBaseDB),
		pendingSubnetValidatorBaseDB: pendingSubnetValidatorBaseDB,
		pendingSubnetValidatorList:   linkeddb.NewDefault(pendingSubnetValidatorBaseDB),
		pendingSubnetDelegatorBaseDB: pendingSubnetDelegatorBaseDB,
		pendingSubnetDelegatorList:   linkeddb.NewDefault(pendingSubnetDelegatorBaseDB),
		validatorWeightDiffsDB:       validatorWeightDiffsDB,
		validatorWeightDiffsCache:    validatorWeightDiffsCache,
		validatorPublicKeyDiffsCache: validatorPublicKeyDiffsCache,
		validatorPublicKeyDiffsDB:    validatorPublicKeyDiffsDB,

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

		transformedSubnets:     make(map[ids.ID]*txs.Tx),
		transformedSubnetCache: transformedSubnetCache,
		transformedSubnetDB:    prefixdb.New(transformedSubnetPrefix, baseDB),

		modifiedSupplies: make(map[ids.ID]uint64),
		supplyCache:      supplyCache,
		supplyDB:         prefixdb.New(supplyPrefix, baseDB),

		addedChains:  make(map[ids.ID][]*txs.Tx),
		chainDB:      prefixdb.New(chainPrefix, baseDB),
		chainCache:   chainCache,
		chainDBCache: chainDBCache,

		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}, nil
}

func (s *state) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.currentStakers.GetValidator(subnetID, nodeID)
}

func (s *state) PutCurrentValidator(staker *Staker) {
	s.currentStakers.PutValidator(staker)
}

func (s *state) UpdateCurrentValidator(staker *Staker) error {
	return s.currentStakers.UpdateValidator(staker)
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

func (s *state) UpdateCurrentDelegator(staker *Staker) error {
	return s.currentStakers.UpdateDelegator(staker)
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

func (s *state) shouldInit() (bool, error) {
	has, err := s.singletonDB.Has(initializedKey)
	return !has, err
}

func (s *state) doneInit() error {
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

func (s *state) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	if tx, exists := s.transformedSubnets[subnetID]; exists {
		return tx, nil
	}

	if tx, cached := s.transformedSubnetCache.Get(subnetID); cached {
		if tx == nil {
			return nil, database.ErrNotFound
		}
		return tx, nil
	}

	transformSubnetTxID, err := database.GetID(s.transformedSubnetDB, subnetID[:])
	if err == database.ErrNotFound {
		s.transformedSubnetCache.Put(subnetID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	transformSubnetTx, _, err := s.GetTx(transformSubnetTxID)
	if err != nil {
		return nil, err
	}
	s.transformedSubnetCache.Put(subnetID, transformSubnetTx)
	return transformSubnetTx, nil
}

func (s *state) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	s.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
}

func (s *state) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	if chains, cached := s.chainCache.Get(subnetID); cached {
		return chains, nil
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
	if chains, cached := s.chainCache.Get(subnetID); cached {
		chains = append(chains, createChainTxIntf)
		s.chainCache.Put(subnetID, chains)
	}
}

func (s *state) getChainDB(subnetID ids.ID) linkeddb.LinkedDB {
	if chainDB, cached := s.chainDBCache.Get(subnetID); cached {
		return chainDB
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
	if tx, cached := s.txCache.Get(txID); cached {
		if tx == nil {
			return nil, status.Unknown, database.ErrNotFound
		}
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
	if _, err := txs.GenesisCodec.Unmarshal(txBytes, &stx); err != nil {
		return nil, status.Unknown, err
	}

	tx, err := txs.Parse(txs.GenesisCodec, stx.Tx)
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

func (s *state) GetRewardConfig(subnetID ids.ID) (reward.Config, error) {
	primaryNetworkCfg := s.cfg.RewardConfig
	if subnetID == constants.PrimaryNetworkID {
		return primaryNetworkCfg, nil
	}

	transformSubnetIntf, err := s.GetSubnetTransformation(subnetID)
	if err == database.ErrNotFound {
		return reward.Config{}, ErrElasticSubnetConfigNotFound
	}
	if err != nil {
		return reward.Config{}, err
	}
	transformSubnet, ok := transformSubnetIntf.Unsigned.(*txs.TransformSubnetTx)
	if !ok {
		return reward.Config{}, errIsNotTransformSubnetTx
	}

	return reward.Config{
		MaxConsumptionRate: transformSubnet.MaxConsumptionRate,
		MinConsumptionRate: transformSubnet.MinConsumptionRate,
		MintingPeriod:      primaryNetworkCfg.MintingPeriod,
		SupplyCap:          transformSubnet.MaximumSupply,
	}, nil
}

func (s *state) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := s.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	if utxos, exists := s.rewardUTXOsCache.Get(txID); exists {
		return utxos, nil
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

func (s *state) GetStartTime(nodeID ids.NodeID, subnetID ids.ID) (time.Time, error) {
	staker, err := s.currentStakers.GetValidator(subnetID, nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return staker.StartTime, nil
}

func (s *state) GetTimestamp() time.Time {
	return s.timestamp
}

func (s *state) SetTimestamp(tm time.Time) {
	s.timestamp = tm
}

func (s *state) GetLastAccepted() ids.ID {
	return s.lastAccepted
}

func (s *state) SetLastAccepted(lastAccepted ids.ID) {
	s.lastAccepted = lastAccepted
}

func (s *state) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	if subnetID == constants.PrimaryNetworkID {
		return s.currentSupply, nil
	}

	supply, ok := s.modifiedSupplies[subnetID]
	if ok {
		return supply, nil
	}

	cachedSupply, ok := s.supplyCache.Get(subnetID)
	if ok {
		if cachedSupply == nil {
			return 0, database.ErrNotFound
		}
		return *cachedSupply, nil
	}

	supply, err := database.GetUInt64(s.supplyDB, subnetID[:])
	if err == database.ErrNotFound {
		s.supplyCache.Put(subnetID, nil)
		return 0, database.ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	s.supplyCache.Put(subnetID, &supply)
	return supply, nil
}

func (s *state) SetCurrentSupply(subnetID ids.ID, cs uint64) {
	if subnetID == constants.PrimaryNetworkID {
		s.currentSupply = cs
	} else {
		s.modifiedSupplies[subnetID] = cs
	}
}

func (s *state) ValidatorSet(subnetID ids.ID, vdrs validators.Set) error {
	for nodeID, validator := range s.currentStakers.validators[subnetID] {
		staker := validator.validator
		if err := vdrs.Add(nodeID, staker.PublicKey, staker.TxID, staker.Weight); err != nil {
			return err
		}

		for _, delegator := range validator.delegators {
			if err := vdrs.AddWeight(nodeID, delegator.Weight); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *state) GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	prefixStruct := heightWithSubnet{
		Height:   height,
		SubnetID: subnetID,
	}
	prefixBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, prefixStruct)
	if err != nil {
		return nil, err
	}
	prefixStr := string(prefixBytes)

	if weightDiffs, ok := s.validatorWeightDiffsCache.Get(prefixStr); ok {
		return weightDiffs, nil
	}

	rawDiffDB := prefixdb.New(prefixBytes, s.validatorWeightDiffsDB)
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
		_, err = blocks.GenesisCodec.Unmarshal(diffIter.Value(), &weightDiff)
		if err != nil {
			return nil, err
		}

		weightDiffs[nodeID] = &weightDiff
	}

	s.validatorWeightDiffsCache.Put(prefixStr, weightDiffs)
	return weightDiffs, diffIter.Error()
}

func (s *state) GetValidatorPublicKeyDiffs(height uint64) (map[ids.NodeID]*bls.PublicKey, error) {
	if publicKeyDiffs, ok := s.validatorPublicKeyDiffsCache.Get(height); ok {
		return publicKeyDiffs, nil
	}

	heightBytes := database.PackUInt64(height)
	rawDiffDB := prefixdb.New(heightBytes, s.validatorPublicKeyDiffsDB)
	diffDB := linkeddb.NewDefault(rawDiffDB)
	diffIter := diffDB.NewIterator()
	defer diffIter.Release()

	pkDiffs := make(map[ids.NodeID]*bls.PublicKey)
	for diffIter.Next() {
		nodeID, err := ids.ToNodeID(diffIter.Key())
		if err != nil {
			return nil, err
		}

		pkBytes := diffIter.Value()
		pk, err := bls.PublicKeyFromBytes(pkBytes)
		if err != nil {
			return nil, err
		}
		pkDiffs[nodeID] = pk
	}

	s.validatorPublicKeyDiffsCache.Put(height, pkDiffs)
	return pkDiffs, diffIter.Error()
}

func (s *state) syncGenesis(genesisBlk blocks.Block, genesis *genesis.State) error {
	genesisBlkID := genesisBlk.ID()
	s.SetLastAccepted(genesisBlkID)
	s.SetTimestamp(time.Unix(int64(genesis.Timestamp), 0))
	s.SetCurrentSupply(constants.PrimaryNetworkID, genesis.InitialSupply)
	s.AddStatelessBlock(genesisBlk)

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

		stakeAmount := tx.Weight()
		stakeDuration := tx.StakingPeriod()
		currentSupply, err := s.GetCurrentSupply(constants.PrimaryNetworkID)
		if err != nil {
			return err
		}

		potentialReward := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := math.Add64(currentSupply, potentialReward)
		if err != nil {
			return err
		}

		// tx is a genesis transactions, hence it's guaranteed to be
		// pre Continuous staking fork. It's fine using tx.StartTime and tx.EndTime
		staker, err := NewCurrentStaker(vdrTx.ID(), tx, tx.StartTime(), tx.EndTime(), potentialReward)
		if err != nil {
			return err
		}

		s.PutCurrentValidator(staker)
		s.AddTx(vdrTx, status.Committed)
		s.SetCurrentSupply(constants.PrimaryNetworkID, newCurrentSupply)
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

	// updateValidators is set to false here to maintain the invariant that the
	// primary network's validator set is empty before the validator sets are
	// initialized.
	return s.write(false /*=updateValidators*/, 0)
}

// Load pulls data previously stored on disk that is expected to be in memory.
func (s *state) load() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.loadMetadata(),
		s.loadCurrentStakers(),
		s.loadPendingStakers(),
		s.initValidatorSets(),
	)
	return errs.Err
}

func (s *state) loadMetadata() error {
	timestamp, err := database.GetTimestamp(s.singletonDB, timestampKey)
	if err != nil {
		return err
	}
	s.persistedTimestamp = timestamp
	s.SetTimestamp(timestamp)

	currentSupply, err := database.GetUInt64(s.singletonDB, currentSupplyKey)
	if err != nil {
		return err
	}
	s.persistedCurrentSupply = currentSupply
	s.SetCurrentSupply(constants.PrimaryNetworkID, currentSupply)

	lastAccepted, err := database.GetID(s.singletonDB, lastAcceptedKey)
	if err != nil {
		return err
	}
	s.persistedLastAccepted = lastAccepted
	s.lastAccepted = lastAccepted
	return nil
}

func (s *state) loadCurrentStakers() error {
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
			return fmt.Errorf("failed loading validator transaction txID %v, %w", txID, err)
		}
		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		metadataBytes := validatorIt.Value()

		var (
			defaultStartTime = time.Time{}
			defaultEndTime   = time.Time{}
		)
		if preStaker, ok := stakerTx.(txs.PreContinuousStakingStaker); ok {
			defaultStartTime = preStaker.StartTime()
			defaultEndTime = preStaker.EndTime()
		}
		metadata := &validatorMetadata{
			txID: txID,
			// use the start values as the fallback
			// in case they are not stored in the database
			// Note: we don't provide [LastUpdated] here because we expect it to
			// always be present on disk.
			StakerStartTime: defaultStartTime.Unix(),
			StakerEndTime:   defaultEndTime.Unix(),
		}
		err = parseValidatorMetadata(metadataBytes, metadata)
		if err != nil {
			return err
		}

		staker, err := NewCurrentStaker(
			txID,
			stakerTx,
			time.Unix(metadata.StakerStartTime, 0),
			time.Unix(metadata.StakerEndTime, 0),
			metadata.PotentialReward)
		if err != nil {
			return err
		}
		UpdateStakingPeriodInPlace(staker, time.Duration(metadata.StakerStakingPeriod))
		IncreaseStakerWeightInPlace(staker, metadata.UpdatedWeight)

		validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.currentStakers.stakers.ReplaceOrInsert(staker)

		s.validatorState.LoadValidatorMetadata(staker.NodeID, staker.SubnetID, metadata)
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

		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
		}

		metadataBytes := subnetValidatorIt.Value()
		var (
			defaultStartTime = time.Time{}
			defaultEndTime   = time.Time{}
		)
		if preStaker, ok := stakerTx.(txs.PreContinuousStakingStaker); ok {
			defaultStartTime = preStaker.StartTime()
			defaultEndTime = preStaker.EndTime()
		}
		metadata := &validatorMetadata{
			txID: txID,
			// use the start values as the fallback
			// in case they are not stored in the database
			StakerStartTime: defaultStartTime.Unix(),
			StakerEndTime:   defaultEndTime.Unix(),
			LastUpdated:     uint64(defaultStartTime.Unix()),
		}
		if err := parseValidatorMetadata(metadataBytes, metadata); err != nil {
			return err
		}

		staker, err := NewCurrentStaker(
			txID,
			stakerTx,
			time.Unix(metadata.StakerStartTime, 0),
			time.Unix(metadata.StakerEndTime, 0),
			metadata.PotentialReward,
		)
		if err != nil {
			return err
		}
		UpdateStakingPeriodInPlace(staker, time.Duration(metadata.StakerStakingPeriod))
		IncreaseStakerWeightInPlace(staker, metadata.UpdatedWeight)

		validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
		validator.validator = staker

		s.currentStakers.stakers.ReplaceOrInsert(staker)

		s.validatorState.LoadValidatorMetadata(staker.NodeID, staker.SubnetID, metadata)
	}

	delegatorIt := s.currentDelegatorList.NewIterator()
	defer delegatorIt.Release()

	subnetDelegatorIt := s.currentSubnetDelegatorList.NewIterator()
	defer subnetDelegatorIt.Release()

	for _, delegatorIt := range []database.Iterator{delegatorIt, subnetDelegatorIt} {
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

			stakerTx, ok := tx.Unsigned.(txs.Staker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			var (
				defaultStartTime = time.Time{}
				defaultEndTime   = time.Time{}
			)
			if preStaker, ok := stakerTx.(txs.PreContinuousStakingStaker); ok {
				defaultStartTime = preStaker.StartTime()
				defaultEndTime = preStaker.EndTime()
			}
			metadata := &delegatorMetadata{
				// use the start values as the fallback
				// in case they are not stored in the database
				StakerStartTime: defaultStartTime.Unix(),
				StakerEndTime:   defaultEndTime.Unix(),
			}
			err = parseDelegatorMetadata(delegatorIt.Value(), metadata)
			if err != nil {
				return err
			}

			staker, err := NewCurrentStaker(
				txID,
				stakerTx,
				time.Unix(metadata.StakerStartTime, 0),
				time.Unix(metadata.StakerEndTime, 0),
				metadata.PotentialReward,
			)
			if err != nil {
				return err
			}
			UpdateStakingPeriodInPlace(staker, time.Duration(metadata.StakerStakingPeriod))
			IncreaseStakerWeightInPlace(staker, metadata.UpdatedWeight)

			validator := s.currentStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			if validator.sortedDelegators == nil {
				validator.sortedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
			}
			validator.delegators[staker.TxID] = staker
			validator.sortedDelegators.ReplaceOrInsert(staker)

			s.currentStakers.stakers.ReplaceOrInsert(staker)
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		validatorIt.Error(),
		subnetValidatorIt.Error(),
		delegatorIt.Error(),
		subnetDelegatorIt.Error(),
	)
	return errs.Err
}

func (s *state) loadPendingStakers() error {
	s.pendingStakers = newBaseStakers()

	validatorIt := s.pendingValidatorList.NewIterator()
	defer validatorIt.Release()

	subnetValidatorIt := s.pendingSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()

	for _, validatorIt := range []database.Iterator{validatorIt, subnetValidatorIt} {
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

			stakerTx, ok := tx.Unsigned.(txs.PreContinuousStakingStaker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			staker, err := NewPendingStaker(txID, stakerTx)
			if err != nil {
				return err
			}

			validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			validator.validator = staker

			s.pendingStakers.stakers.ReplaceOrInsert(staker)
		}
	}

	delegatorIt := s.pendingDelegatorList.NewIterator()
	defer delegatorIt.Release()

	subnetDelegatorIt := s.pendingSubnetDelegatorList.NewIterator()
	defer subnetDelegatorIt.Release()

	for _, delegatorIt := range []database.Iterator{delegatorIt, subnetDelegatorIt} {
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

			stakerTx, ok := tx.Unsigned.(txs.PreContinuousStakingStaker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			staker, err := NewPendingStaker(txID, stakerTx)
			if err != nil {
				return err
			}

			validator := s.pendingStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			if validator.sortedDelegators == nil {
				validator.sortedDelegators = btree.NewG(defaultTreeDegree, (*Staker).Less)
			}
			validator.delegators[staker.TxID] = staker
			validator.sortedDelegators.ReplaceOrInsert(staker)

			s.pendingStakers.stakers.ReplaceOrInsert(staker)
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		validatorIt.Error(),
		subnetValidatorIt.Error(),
		delegatorIt.Error(),
		subnetDelegatorIt.Error(),
	)
	return errs.Err
}

// Invariant: initValidatorSets requires loadCurrentValidators to have already
// been called.
func (s *state) initValidatorSets() error {
	primaryValidators, ok := s.cfg.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		return errMissingValidatorSet
	}
	if primaryValidators.Len() != 0 {
		// Enforce the invariant that the validator set is empty here.
		return errValidatorSetAlreadyPopulated
	}
	err := s.ValidatorSet(constants.PrimaryNetworkID, primaryValidators)
	if err != nil {
		return err
	}

	vl := validators.NewLogger(s.ctx.Log, s.bootstrapped, constants.PrimaryNetworkID, s.ctx.NodeID)
	primaryValidators.RegisterCallbackListener(vl)

	s.metrics.SetLocalStake(primaryValidators.GetWeight(s.ctx.NodeID))
	s.metrics.SetTotalStake(primaryValidators.Weight())

	for subnetID := range s.cfg.TrackedSubnets {
		subnetValidators := validators.NewSet()
		err := s.ValidatorSet(subnetID, subnetValidators)
		if err != nil {
			return err
		}

		if !s.cfg.Validators.Add(subnetID, subnetValidators) {
			return fmt.Errorf("%w: %s", errDuplicateValidatorSet, subnetID)
		}

		vl := validators.NewLogger(s.ctx.Log, s.bootstrapped, subnetID, s.ctx.NodeID)
		subnetValidators.RegisterCallbackListener(vl)
	}
	return nil
}

func (s *state) write(updateValidators bool, height uint64) error {
	errs := wrappers.Errs{}
	errs.Add(
		s.writeBlocks(),
		s.writeCurrentStakers(updateValidators, height),
		s.writePendingStakers(),
		s.WriteValidatorMetadata(s.currentValidatorList, s.currentSubnetValidatorList), // Must be called after writeCurrentStakers
		s.writeTXs(),
		s.writeRewardUTXOs(),
		s.writeUTXOs(),
		s.writeSubnets(),
		s.writeTransformedSubnets(),
		s.writeSubnetSupplies(),
		s.writeChains(),
		s.writeMetadata(),
	)
	return errs.Err
}

func (s *state) Close() error {
	errs := wrappers.Errs{}
	errs.Add(
		s.pendingSubnetValidatorBaseDB.Close(),
		s.pendingSubnetDelegatorBaseDB.Close(),
		s.pendingDelegatorBaseDB.Close(),
		s.pendingValidatorBaseDB.Close(),
		s.pendingValidatorsDB.Close(),
		s.currentSubnetValidatorBaseDB.Close(),
		s.currentSubnetDelegatorBaseDB.Close(),
		s.currentDelegatorBaseDB.Close(),
		s.currentValidatorBaseDB.Close(),
		s.currentValidatorsDB.Close(),
		s.validatorsDB.Close(),
		s.txDB.Close(),
		s.rewardUTXODB.Close(),
		s.utxoDB.Close(),
		s.subnetBaseDB.Close(),
		s.transformedSubnetDB.Close(),
		s.supplyDB.Close(),
		s.chainDB.Close(),
		s.singletonDB.Close(),
		s.blockDB.Close(),
	)
	return errs.Err
}

func (s *state) sync(genesis []byte) error {
	shouldInit, err := s.shouldInit()
	if err != nil {
		return fmt.Errorf(
			"failed to check if the database is initialized: %w",
			err,
		)
	}

	// If the database is empty, create the platform chain anew using the
	// provided genesis state
	if shouldInit {
		if err := s.init(genesis); err != nil {
			return fmt.Errorf(
				"failed to initialize the database: %w",
				err,
			)
		}
	}

	if err := s.load(); err != nil {
		return fmt.Errorf(
			"failed to load the database state: %w",
			err,
		)
	}
	return nil
}

func (s *state) init(genesisBytes []byte) error {
	// Create the genesis block and save it as being accepted (We don't do
	// genesisBlock.Accept() because then it'd look for genesisBlock's
	// non-existent parent)
	genesisID := hashing.ComputeHash256Array(genesisBytes)
	genesisBlock, err := blocks.NewApricotCommitBlock(genesisID, 0 /*height*/)
	if err != nil {
		return err
	}

	genesisState, err := genesis.ParseState(genesisBytes)
	if err != nil {
		return err
	}
	if err := s.syncGenesis(genesisBlock, genesisState); err != nil {
		return err
	}

	if err := s.doneInit(); err != nil {
		return err
	}

	return s.Commit()
}

func (s *state) AddStatelessBlock(block blocks.Block) {
	s.addedBlocks[block.ID()] = block
}

func (s *state) SetHeight(height uint64) {
	s.currentHeight = height
}

func (s *state) Commit() error {
	defer s.Abort()
	batch, err := s.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (s *state) Abort() {
	s.baseDB.Abort()
}

func (s *state) Checksum() ids.ID {
	return s.utxoState.Checksum()
}

func (s *state) CommitBatch() (database.Batch, error) {
	// updateValidators is set to true here so that the validator manager is
	// kept up to date with the last accepted state.
	if err := s.write(true /*=updateValidators*/, s.currentHeight); err != nil {
		return nil, err
	}
	return s.baseDB.CommitBatch()
}

func (s *state) writeBlocks() error {
	for blkID, blk := range s.addedBlocks {
		blkID := blkID

		stBlk := stateBlk{
			Blk:    blk,
			Bytes:  blk.Bytes(),
			Status: choices.Accepted,
		}

		// Note: blocks to be stored are verified, so it's safe to marshal them with GenesisCodec
		blockBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, &stBlk)
		if err != nil {
			return fmt.Errorf("failed to marshal block %s to store: %w", blkID, err)
		}

		delete(s.addedBlocks, blkID)
		// Note: Evict is used rather than Put here because blk may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.blockCache.Evict(blkID)
		if err := s.blockDB.Put(blkID[:], blockBytes); err != nil {
			return fmt.Errorf("failed to write block %s: %w", blkID, err)
		}
	}
	return nil
}

func (s *state) GetStatelessBlock(blockID ids.ID) (blocks.Block, error) {
	if blk, exists := s.addedBlocks[blockID]; exists {
		return blk, nil
	}
	if blk, cached := s.blockCache.Get(blockID); cached {
		if blk == nil {
			return nil, database.ErrNotFound
		}

		return blk, nil
	}

	blkBytes, err := s.blockDB.Get(blockID[:])
	if err == database.ErrNotFound {
		s.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	blk, status, _, err := parseStoredBlock(blkBytes)
	if err != nil {
		return nil, err
	}

	if status != choices.Accepted {
		s.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound
	}

	s.blockCache.Put(blockID, blk)
	return blk, nil
}

func (s *state) writeCurrentStakers(updateValidators bool, height uint64) error {
	heightBytes := database.PackUInt64(height)
	rawPublicKeyDiffDB := prefixdb.New(heightBytes, s.validatorPublicKeyDiffsDB)
	pkDiffDB := linkeddb.NewDefault(rawPublicKeyDiffDB)
	// Node ID --> BLS public key of node before it left the validator set.
	pkDiffs := make(map[ids.NodeID]*bls.PublicKey)

	for subnetID, validatorDiffs := range s.currentStakers.validatorDiffs {
		delete(s.currentStakers.validatorDiffs, subnetID)

		// Select db to write to
		validatorDB := s.currentSubnetValidatorList
		delegatorDB := s.currentSubnetDelegatorList
		if subnetID == constants.PrimaryNetworkID {
			validatorDB = s.currentValidatorList
			delegatorDB = s.currentDelegatorList
		}

		prefixStruct := heightWithSubnet{
			Height:   height,
			SubnetID: subnetID,
		}
		prefixBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, prefixStruct)
		if err != nil {
			return fmt.Errorf("failed to create prefix bytes: %w", err)
		}
		rawWeightDiffDB := prefixdb.New(prefixBytes, s.validatorWeightDiffsDB)
		weightDiffDB := linkeddb.NewDefault(rawWeightDiffDB)
		weightDiffs := make(map[ids.NodeID]*ValidatorWeightDiff)

		// Record the change in weight and/or public key for each validator.
		for nodeID, validatorDiff := range validatorDiffs {
			// Copy [nodeID] so it doesn't get overwritten next iteration.
			nodeID := nodeID
			validator := validatorDiff.validator.staker
			weightDiff := &ValidatorWeightDiff{}

			switch validatorDiff.validator.status {
			case added:
				weightDiff = &ValidatorWeightDiff{
					Decrease: false,
					Amount:   validator.Weight,
				}

				// The validator is being added.
				//
				// Invariant: It's impossible for a delegator to have been
				// rewarded in the same block that the validator was added.
				metadata := &validatorMetadata{
					txID:        validator.TxID,
					lastUpdated: validator.StartTime,

					UpDuration:               0,
					LastUpdated:              uint64(validator.StartTime.Unix()),
					PotentialReward:          validator.PotentialReward,
					PotentialDelegateeReward: 0,

					// a staker update may change its times and weight wrt those
					// specified in the tx creating the staker. We store these data
					// to properly reconstruct staker data.
					StakerStartTime:     validator.StartTime.Unix(),
					StakerStakingPeriod: int64(validator.CurrentStakingPeriod()),
					StakerEndTime:       validator.EndTime.Unix(),
					UpdatedWeight:       validator.Weight,
				}

				// Let's start using V1 as soon as we deploy code. No need to
				// wait till Continuous staking fork activation to do that.
				metadataBytes, err := stakersMetadataCodec.Marshal(stakerMetadataCodecV1, metadata)
				if err != nil {
					return fmt.Errorf("failed to serialize validator metadata: %w", err)
				}
				if err = validatorDB.Put(validator.TxID[:], metadataBytes); err != nil {
					return fmt.Errorf("failed to write validator metadata to list: %w", err)
				}
				s.validatorState.LoadValidatorMetadata(nodeID, subnetID, metadata)

			case deleted:
				weightDiff = &ValidatorWeightDiff{
					Decrease: true,
					Amount:   validator.Weight,
				}

				// Invariant: Only the Primary Network contains non-nil
				//            public keys.
				if validator.PublicKey != nil {
					// Record the public key of the validator being removed.
					pkDiffs[nodeID] = validator.PublicKey

					pkBytes := bls.PublicKeyToBytes(validator.PublicKey)
					if err := pkDiffDB.Put(nodeID[:], pkBytes); err != nil {
						return err
					}
				}

				if err := validatorDB.Delete(validator.TxID[:]); err != nil {
					return fmt.Errorf("failed to delete current staker: %w", err)
				}

				s.validatorState.DeleteValidatorMetadata(nodeID, subnetID)
			case updated:
				// load current metadata and duly update them, following validator update
				metadataBytes, err := validatorDB.Get(validator.TxID[:])
				if err != nil {
					return fmt.Errorf("failed to get metadata of updated validator: %w", err)
				}

				metadata := &validatorMetadata{
					txID: validator.TxID,
				}
				if err := parseValidatorMetadata(metadataBytes, metadata); err != nil {
					return err
				}

				// build weight diff by comparing current weight with previously stored one
				switch {
				case validator.Weight > metadata.UpdatedWeight:
					if err := weightDiff.Add(false, validator.Weight-metadata.UpdatedWeight); err != nil {
						return fmt.Errorf("failed to increase node weight diff: %w", err)
					}
				case validator.Weight < metadata.UpdatedWeight:
					if err := weightDiff.Add(true, metadata.UpdatedWeight-validator.Weight); err != nil {
						return fmt.Errorf("failed to decrease node weight diff: %w", err)
					}
				default:
					// no weight changes, nothing to do
				}

				metadata.lastUpdated = validator.StartTime
				metadata.StakerStartTime = validator.StartTime.Unix()
				metadata.StakerStakingPeriod = int64(validator.CurrentStakingPeriod())
				metadata.StakerEndTime = validator.EndTime.Unix()
				metadata.LastUpdated = uint64(metadata.StakerStartTime)
				metadata.UpdatedWeight = validator.Weight

				metadataBytes, err = stakersMetadataCodec.Marshal(stakerMetadataCodecV1, metadata)
				if err != nil {
					return fmt.Errorf("failed to serialize validator metadata: %w", err)
				}
				if err = validatorDB.Put(validator.TxID[:], metadataBytes); err != nil {
					return fmt.Errorf("failed to write validator metadata to list: %w", err)
				}
				if err := s.validatorState.UpdateValidatorMetadata(nodeID, subnetID, metadata); err != nil {
					return fmt.Errorf("failed updating validator metadata: %w", err)
				}

				// in current implementation, all delegatee reward are paid back to the user
				// as soon as the validator is rewarded and shifted. Hence we clean it up if available.
				err = s.validatorState.DeleteDelegateeReward(subnetID, nodeID)
				if err != nil && !errors.Is(err, database.ErrNotFound) {
					return fmt.Errorf("failed to delete delegatee reward: %w", err)
				}

			case unmodified:
				// nothing to do
			default:
				return ErrUnknownStakerStatus
			}

			err := writeCurrentDelegatorDiff(
				delegatorDB,
				weightDiff,
				validatorDiff,
			)
			if err != nil {
				return err
			}

			if weightDiff.Amount == 0 {
				// No weight change to record; go to next validator.
				continue
			}
			weightDiffs[nodeID] = weightDiff

			weightDiffBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, weightDiff)
			if err != nil {
				return fmt.Errorf("failed to serialize validator weight diff: %w", err)
			}

			if err := weightDiffDB.Put(nodeID[:], weightDiffBytes); err != nil {
				return err
			}

			// TODO: Move the validator set management out of the state package
			if !updateValidators {
				continue
			}

			// We only track the current validator set of tracked subnets.
			if subnetID != constants.PrimaryNetworkID && !s.cfg.TrackedSubnets.Contains(subnetID) {
				continue
			}

			if weightDiff.Decrease {
				err = validators.RemoveWeight(s.cfg.Validators, subnetID, nodeID, weightDiff.Amount)
			} else {
				if validatorDiff.validator.status == added {
					staker := validatorDiff.validator.staker
					err = validators.Add(
						s.cfg.Validators,
						subnetID,
						nodeID,
						staker.PublicKey,
						staker.TxID,
						weightDiff.Amount,
					)
				} else {
					err = validators.AddWeight(s.cfg.Validators, subnetID, nodeID, weightDiff.Amount)
				}
			}
			if err != nil {
				return fmt.Errorf("failed to update validator weight: %w", err)
			}
		}
		s.validatorWeightDiffsCache.Put(string(prefixBytes), weightDiffs)
	}
	s.validatorPublicKeyDiffsCache.Put(height, pkDiffs)

	// TODO: Move validator set management out of the state package
	//
	// Attempt to update the stake metrics
	if !updateValidators {
		return nil
	}
	primaryValidators, ok := s.cfg.Validators.Get(constants.PrimaryNetworkID)
	if !ok {
		return nil
	}
	s.metrics.SetLocalStake(primaryValidators.GetWeight(s.ctx.NodeID))
	s.metrics.SetTotalStake(primaryValidators.Weight())
	return nil
}

func writeCurrentDelegatorDiff(
	currentDelegatorList linkeddb.LinkedDB,
	weightDiff *ValidatorWeightDiff,
	validatorDiff *diffValidator,
) error {
	for _, ds := range validatorDiff.delegators {
		delegator := ds.staker
		switch ds.status {
		case added:
			if err := weightDiff.Add(false, delegator.Weight); err != nil {
				return fmt.Errorf("failed to increase node weight diff: %w", err)
			}

			// Let's start using V1 as soon as we deploy code. No need to
			// wait till Continuous staking fork activation to do that.
			metadata := &delegatorMetadata{
				PotentialReward:     delegator.PotentialReward,
				StakerStartTime:     delegator.StartTime.Unix(),
				StakerStakingPeriod: int64(delegator.CurrentStakingPeriod()),
				StakerEndTime:       delegator.EndTime.Unix(),
				UpdatedWeight:       delegator.Weight,
			}
			metadataBytes, err := stakersMetadataCodec.Marshal(stakerMetadataCodecV1, metadata)
			if err != nil {
				return fmt.Errorf("failed marshalling delegators metadata: %w", err)
			}
			err = currentDelegatorList.Put(delegator.TxID[:], metadataBytes)
			if err != nil {
				return fmt.Errorf("failed to write current delegator to list: %w", err)
			}
		case deleted:
			if err := weightDiff.Add(true, delegator.Weight); err != nil {
				return fmt.Errorf("failed to decrease node weight diff: %w", err)
			}

			if err := currentDelegatorList.Delete(delegator.TxID[:]); err != nil {
				return fmt.Errorf("failed to delete current staker: %w", err)
			}
		case updated:
			// load current metadata and duly update them, following staker update
			metadataBytes, err := currentDelegatorList.Get(delegator.TxID[:])
			if err != nil {
				return fmt.Errorf("failed to get metadata of updated delegator: %w", err)
			}

			metadata := &delegatorMetadata{}
			if err := parseDelegatorMetadata(metadataBytes, metadata); err != nil {
				return err
			}

			switch {
			case delegator.Weight > metadata.UpdatedWeight:
				if err := weightDiff.Add(false, delegator.Weight-metadata.UpdatedWeight); err != nil {
					return fmt.Errorf("failed to increase node weight diff: %w", err)
				}
			case delegator.Weight < metadata.UpdatedWeight:
				if err := weightDiff.Add(true, metadata.UpdatedWeight-delegator.Weight); err != nil {
					return fmt.Errorf("failed to decrease node weight diff: %w", err)
				}
			default:
				// no weight changes, nothing to do
			}

			metadata.StakerStartTime = delegator.StartTime.Unix()
			metadata.StakerStakingPeriod = int64(delegator.CurrentStakingPeriod())
			metadata.StakerEndTime = delegator.EndTime.Unix()
			metadata.UpdatedWeight = delegator.Weight

			metadataBytes, err = stakersMetadataCodec.Marshal(stakerMetadataCodecV1, metadata)
			if err != nil {
				return fmt.Errorf("failed to serialize delegator metadata: %w", err)
			}

			if err = currentDelegatorList.Put(delegator.TxID[:], metadataBytes); err != nil {
				return fmt.Errorf("failed to write delegator metadata to list: %w", err)
			}

		case unmodified:
			// nothing to do
		default:
			return ErrUnknownStakerStatus
		}
	}
	return nil
}

func (s *state) writePendingStakers() error {
	for subnetID, subnetValidatorDiffs := range s.pendingStakers.validatorDiffs {
		delete(s.pendingStakers.validatorDiffs, subnetID)

		validatorDB := s.pendingSubnetValidatorList
		delegatorDB := s.pendingSubnetDelegatorList
		if subnetID == constants.PrimaryNetworkID {
			validatorDB = s.pendingValidatorList
			delegatorDB = s.pendingDelegatorList
		}

		for _, validatorDiff := range subnetValidatorDiffs {
			err := writePendingDiff(
				validatorDB,
				delegatorDB,
				validatorDiff,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func writePendingDiff(
	pendingValidatorList linkeddb.LinkedDB,
	pendingDelegatorList linkeddb.LinkedDB,
	validatorDiff *diffValidator,
) error {
	switch validatorDiff.validator.status {
	case added:
		err := pendingValidatorList.Put(validatorDiff.validator.staker.TxID[:], nil)
		if err != nil {
			return fmt.Errorf("failed to add pending validator: %w", err)
		}
	case deleted:
		err := pendingValidatorList.Delete(validatorDiff.validator.staker.TxID[:])
		if err != nil {
			return fmt.Errorf("failed to delete pending validator: %w", err)
		}
	case updated, unmodified:
		// nothing to do
	default:
		return ErrUnknownStakerStatus
	}

	for _, ds := range validatorDiff.delegators {
		delegator := ds.staker
		switch ds.status {
		case added:
			if err := pendingDelegatorList.Put(delegator.TxID[:], nil); err != nil {
				return fmt.Errorf("failed to write pending delegator to list: %w", err)
			}
		case deleted:
			if err := pendingDelegatorList.Delete(delegator.TxID[:]); err != nil {
				return fmt.Errorf("failed to delete pending delegator: %w", err)
			}
		case updated, unmodified:
			// nothing to do
		default:
			return ErrUnknownStakerStatus
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

		// Note that we're serializing a [txBytesAndStatus] here, not a
		// *txs.Tx, so we don't use [txs.Codec].
		txBytes, err := txs.GenesisCodec.Marshal(txs.Version, &stx)
		if err != nil {
			return fmt.Errorf("failed to serialize tx: %w", err)
		}

		delete(s.addedTxs, txID)
		// Note: Evict is used rather than Put here because stx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.txCache.Evict(txID)
		if err := s.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to add tx: %w", err)
		}
	}
	return nil
}

func (s *state) writeRewardUTXOs() error {
	for txID, utxos := range s.addedRewardUTXOs {
		delete(s.addedRewardUTXOs, txID)

		// following continuous staking fork it's key not to overwrite
		// utxos from previous staking periods which may already cached.
		preUtxos, found := s.rewardUTXOsCache.Get(txID)
		if found {
			preUtxos = append(preUtxos, utxos...)
			s.rewardUTXOsCache.Put(txID, preUtxos)
		} else {
			s.rewardUTXOsCache.Put(txID, utxos)
		}

		rawTxDB := prefixdb.New(txID[:], s.rewardUTXODB)
		txDB := linkeddb.NewDefault(rawTxDB)

		for _, utxo := range utxos {
			utxoBytes, err := txs.GenesisCodec.Marshal(txs.Version, utxo)
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

func (s *state) writeTransformedSubnets() error {
	for subnetID, tx := range s.transformedSubnets {
		txID := tx.ID()

		delete(s.transformedSubnets, subnetID)
		// Note: Evict is used rather than Put here because tx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.transformedSubnetCache.Evict(subnetID)
		if err := database.PutID(s.transformedSubnetDB, subnetID[:], txID); err != nil {
			return fmt.Errorf("failed to write transformed subnet: %w", err)
		}
	}
	return nil
}

func (s *state) writeSubnetSupplies() error {
	for subnetID, supply := range s.modifiedSupplies {
		supply := supply
		delete(s.modifiedSupplies, subnetID)
		s.supplyCache.Put(subnetID, &supply)
		if err := database.PutUInt64(s.supplyDB, subnetID[:], supply); err != nil {
			return fmt.Errorf("failed to write subnet supply: %w", err)
		}
	}
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
	if !s.persistedTimestamp.Equal(s.timestamp) {
		if err := database.PutTimestamp(s.singletonDB, timestampKey, s.timestamp); err != nil {
			return fmt.Errorf("failed to write timestamp: %w", err)
		}
		s.persistedTimestamp = s.timestamp
	}
	if s.persistedCurrentSupply != s.currentSupply {
		if err := database.PutUInt64(s.singletonDB, currentSupplyKey, s.currentSupply); err != nil {
			return fmt.Errorf("failed to write current supply: %w", err)
		}
		s.persistedCurrentSupply = s.currentSupply
	}
	if s.persistedLastAccepted != s.lastAccepted {
		if err := database.PutID(s.singletonDB, lastAcceptedKey, s.lastAccepted); err != nil {
			return fmt.Errorf("failed to write last accepted: %w", err)
		}
		s.persistedLastAccepted = s.lastAccepted
	}
	return nil
}

// Returns the block, status of the block, and whether it is a [stateBlk].
// Invariant: blkBytes is safe to parse with blocks.GenesisCodec
//
// TODO: Remove after v1.11.x is activated
func parseStoredBlock(blkBytes []byte) (blocks.Block, choices.Status, bool, error) {
	// Attempt to parse as blocks.Block
	blk, err := blocks.Parse(blocks.GenesisCodec, blkBytes)
	if err == nil {
		return blk, choices.Accepted, false, nil
	}

	// Fallback to [stateBlk]
	blkState := stateBlk{}
	if _, err := blocks.GenesisCodec.Unmarshal(blkBytes, &blkState); err != nil {
		return nil, choices.Processing, false, err
	}

	blkState.Blk, err = blocks.Parse(blocks.GenesisCodec, blkState.Bytes)
	if err != nil {
		return nil, choices.Processing, false, err
	}

	return blkState.Blk, blkState.Status, true, nil
}
