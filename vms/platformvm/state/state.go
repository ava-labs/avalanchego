// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

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
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/x/merkledb"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	HistoryLength = uint(256)

	valueNodeCacheSize        = 512 * units.MiB
	intermediateNodeCacheSize = 512 * units.MiB
	utxoCacheSize             = 8192 // from avax/utxo_state.go
)

var (
	_ State = (*state)(nil)

	errValidatorSetAlreadyPopulated = errors.New("validator set already populated")
	errIsNotSubnet                  = errors.New("is not a subnet")

	merkleStatePrefix       = []byte{0x00}
	merkleSingletonPrefix   = []byte{0x01}
	merkleBlockPrefix       = []byte{0x02}
	merkleBlockIDsPrefix    = []byte{0x03}
	merkleTxPrefix          = []byte{0x04}
	merkleIndexUTXOsPrefix  = []byte{0x05} // to serve UTXOIDs(addr)
	merkleUptimesPrefix     = []byte{0x06} // locally measured uptimes
	merkleWeightDiffPrefix  = []byte{0x07} // non-merkleized validators weight diff. TODO: should we merkleize them?
	merkleBlsKeyDiffPrefix  = []byte{0x08}
	merkleRewardUtxosPrefix = []byte{0x09}

	initializedKey = []byte("initialized")

	// merkle db sections
	metadataSectionPrefix      = byte(0x00)
	merkleChainTimeKey         = []byte{metadataSectionPrefix, 0x00}
	merkleLastAcceptedBlkIDKey = []byte{metadataSectionPrefix, 0x01}
	merkleSuppliesPrefix       = []byte{metadataSectionPrefix, 0x02}

	permissionedSubnetSectionPrefix = []byte{0x01}
	elasticSubnetSectionPrefix      = []byte{0x02}
	chainsSectionPrefix             = []byte{0x03}
	utxosSectionPrefix              = []byte{0x04}
	currentStakersSectionPrefix     = []byte{0x05}
	pendingStakersSectionPrefix     = []byte{0x06}
	delegateeRewardsPrefix          = []byte{0x07}
	subnetOwnersPrefix              = []byte{0x08}
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

	GetSubnetOwner(subnetID ids.ID) (fx.Owner, error)
	SetSubnetOwner(subnetID ids.ID, owner fx.Owner)

	GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error)
	AddSubnetTransformation(transformSubnetTx *txs.Tx)

	GetChains(subnetID ids.ID) ([]*txs.Tx, error)
	AddChain(createChainTx *txs.Tx)

	GetTx(txID ids.ID) (*txs.Tx, status.Status, error)
	AddTx(tx *txs.Tx, status status.Status)
}

type State interface {
	Chain
	uptime.State
	avax.UTXOReader

	GetLastAccepted() ids.ID
	SetLastAccepted(blkID ids.ID)

	GetStatelessBlock(blockID ids.ID) (block.Block, error)

	// Invariant: [block] is an accepted block.
	AddStatelessBlock(block block.Block)

	GetBlockIDAtHeight(height uint64) (ids.ID, error)

	// ApplyValidatorWeightDiffs iterates from [startHeight] towards the genesis
	// block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// will typically be greater than or equal to [endHeight]. If [startHeight]
	// is less than [endHeight], no diffs will be applied.
	ApplyValidatorWeightDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
		subnetID ids.ID,
	) error

	// ApplyValidatorPublicKeyDiffs iterates from [startHeight] towards the
	// genesis block until it has applied all of the diffs up to and including
	// [endHeight]. Applying the diffs modifies [validators].
	//
	// Invariant: If attempting to generate the validator set for
	// [endHeight - 1], [validators] must initially contain the validator
	// weights for [startHeight].
	//
	// Note: Because this function iterates towards the genesis, [startHeight]
	// will typically be greater than or equal to [endHeight]. If [startHeight]
	// is less than [endHeight], no diffs will be applied.
	ApplyValidatorPublicKeyDiffs(
		ctx context.Context,
		validators map[ids.NodeID]*validators.GetValidatorOutput,
		startHeight uint64,
		endHeight uint64,
	) error

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
	Blk    block.Block
	Bytes  []byte         `serialize:"true"`
	Status choices.Status `serialize:"true"`
}

// Stores global state in a merkle trie. This means that each state corresponds
// to a unique merkle root. Specifically, the following state is merkleized.
// - Delegatee Rewards
// - UTXOs
// - Current Supply
// - Subnet Creation Transactions
// - Subnet Owners
// - Subnet Transformation Transactions
// - Chain Creation Transactions
// - Chain time
// - Last Accepted Block ID
// - Current Staker Set
// - Pending Staker Set
//
// Changing any of the above state will cause the merkle root to change.
//
// The following state is not merkleized:
// - Database Initialization Status
// - Blocks
// - Block IDs
// - Transactions (note some transactions are also stored merkleized)
// - Uptimes
// - Weight Diffs
// - BLS Key Diffs
// - Reward UTXOs
type state struct {
	validators validators.Manager
	ctx        *snow.Context
	metrics    metrics.Metrics
	rewards    reward.Calculator

	baseDB       *versiondb.Database
	singletonDB  database.Database
	baseMerkleDB database.Database
	merkleDB     merkledb.MerkleDB // Stores merkleized state

	// stakers section (missing Delegatee piece)
	// TODO: Consider moving delegatee to UTXOs section
	currentStakers *baseStakers
	pendingStakers *baseStakers

	delegateeRewardCache    map[ids.NodeID]map[ids.ID]uint64 // (nodeID, subnetID) --> delegatee amount
	modifiedDelegateeReward map[ids.NodeID]set.Set[ids.ID]   // tracks (nodeID, subnetID) pairs updated after last commit

	// UTXOs section
	modifiedUTXOs map[ids.ID]*avax.UTXO            // map of UTXO ID -> *UTXO
	utxoCache     cache.Cacher[ids.ID, *avax.UTXO] // UTXO ID -> *UTXO. If the *UTXO is nil the UTXO doesn't exist

	// Metadata section
	chainTime, latestComittedChainTime                  time.Time
	lastAcceptedBlkID, latestCommittedLastAcceptedBlkID ids.ID
	lastAcceptedHeight                                  uint64                        // TODO: Should this be written to state??
	modifiedSupplies                                    map[ids.ID]uint64             // map of subnetID -> current supply
	suppliesCache                                       cache.Cacher[ids.ID, *uint64] // cache of subnetID -> current supply if the entry is nil, it is not in the database

	// Subnets section
	// Subnet ID --> Owner of the subnet
	subnetOwners     map[ids.ID]fx.Owner
	subnetOwnerCache cache.Cacher[ids.ID, fxOwnerAndSize] // cache of subnetID -> owner if the entry is nil, it is not in the database

	addedPermissionedSubnets []*txs.Tx                     // added SubnetTxs, waiting to be committed
	permissionedSubnetCache  []*txs.Tx                     // nil if the subnets haven't been loaded
	addedElasticSubnets      map[ids.ID]*txs.Tx            // map of subnetID -> transformSubnetTx
	elasticSubnetCache       cache.Cacher[ids.ID, *txs.Tx] // cache of subnetID -> transformSubnetTx if the entry is nil, it is not in the database

	// Chains section
	addedChains map[ids.ID][]*txs.Tx            // maps subnetID -> the newly added chains to the subnet
	chainCache  cache.Cacher[ids.ID, []*txs.Tx] // cache of subnetID -> the chains after all local modifications []*txs.Tx

	// Blocks section
	// Note: addedBlocks is a list because multiple blocks can be committed at one (proposal + accepted option)
	addedBlocks map[ids.ID]block.Block            // map of blockID -> Block.
	blockCache  cache.Cacher[ids.ID, block.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

	addedBlockIDs map[uint64]ids.ID            // map of height -> blockID
	blockIDCache  cache.Cacher[uint64, ids.ID] // cache of height -> blockID. If the entry is ids.Empty, it is not in the database
	blockIDDB     database.Database

	// Txs section
	// FIND a way to reduce use of these. No use in verification of addedTxs
	// a limited windows to support APIs
	addedTxs map[ids.ID]*txAndStatus            // map of txID -> {*txs.Tx, Status}
	txCache  cache.Cacher[ids.ID, *txAndStatus] // txID -> {*txs.Tx, Status}. If the entry is nil, it isn't in the database
	txDB     database.Database

	indexedUTXOsDB database.Database

	localUptimesCache    map[ids.NodeID]map[ids.ID]*uptimes // vdrID -> subnetID -> metadata
	modifiedLocalUptimes map[ids.NodeID]set.Set[ids.ID]     // vdrID -> subnetIDs
	localUptimesDB       database.Database

	flatValidatorWeightDiffsDB    database.Database
	flatValidatorPublicKeyDiffsDB database.Database

	// Reward UTXOs section
	addedRewardUTXOs map[ids.ID][]*avax.UTXO            // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher[ids.ID, []*avax.UTXO] // txID -> []*UTXO
	rewardUTXOsDB    database.Database
}

type ValidatorWeightDiff struct {
	Decrease bool   `serialize:"true"`
	Amount   uint64 `serialize:"true"`
}

func (v *ValidatorWeightDiff) Add(negative bool, amount uint64) error {
	if v.Decrease == negative {
		var err error
		v.Amount, err = safemath.Add64(v.Amount, amount)
		return err
	}

	if v.Amount > amount {
		v.Amount -= amount
	} else {
		v.Amount = safemath.AbsDiff(v.Amount, amount)
		v.Decrease = negative
	}
	return nil
}

type txBytesAndStatus struct {
	Tx     []byte        `serialize:"true"`
	Status status.Status `serialize:"true"`
}

type txAndStatus struct {
	tx     *txs.Tx
	status status.Status
}

type fxOwnerAndSize struct {
	owner fx.Owner
	size  int
}

func txSize(_ ids.ID, tx *txs.Tx) int {
	if tx == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(tx.Bytes()) + constants.PointerOverhead
}

func txAndStatusSize(_ ids.ID, t *txAndStatus) int {
	if t == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(t.tx.Bytes()) + wrappers.IntLen + 2*constants.PointerOverhead
}

func blockSize(_ ids.ID, blk block.Block) int {
	if blk == nil {
		return ids.IDLen + constants.PointerOverhead
	}
	return ids.IDLen + len(blk.Bytes()) + constants.PointerOverhead
}

func New(
	db database.Database,
	genesisBytes []byte,
	metricsReg prometheus.Registerer,
	validators validators.Manager,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metrics metrics.Metrics,
	rewards reward.Calculator,
) (State, error) {
	s, err := newState(
		db,
		metrics,
		validators,
		execCfg,
		ctx,
		metricsReg,
		rewards,
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

func newState(
	db database.Database,
	metrics metrics.Metrics,
	validators validators.Manager,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metricsReg prometheus.Registerer,
	rewards reward.Calculator,
) (*state, error) {
	var (
		baseDB                        = versiondb.New(db)
		baseMerkleDB                  = prefixdb.New(merkleStatePrefix, baseDB)
		singletonDB                   = prefixdb.New(merkleSingletonPrefix, baseDB)
		blockDB                       = prefixdb.New(merkleBlockPrefix, baseDB)
		blockIDsDB                    = prefixdb.New(merkleBlockIDsPrefix, baseDB)
		txDB                          = prefixdb.New(merkleTxPrefix, baseDB)
		indexedUTXOsDB                = prefixdb.New(merkleIndexUTXOsPrefix, baseDB)
		localUptimesDB                = prefixdb.New(merkleUptimesPrefix, baseDB)
		flatValidatorWeightDiffsDB    = prefixdb.New(merkleWeightDiffPrefix, baseDB)
		flatValidatorPublicKeyDiffsDB = prefixdb.New(merkleBlsKeyDiffPrefix, baseDB)
		rewardUTXOsDB                 = prefixdb.New(merkleRewardUtxosPrefix, baseDB)
	)

	noOpTracer, err := trace.New(trace.Config{Enabled: false})
	if err != nil {
		return nil, fmt.Errorf("failed creating noOpTraces: %w", err)
	}

	merkleDB, err := merkledb.New(context.TODO(), baseMerkleDB, merkledb.Config{
		BranchFactor:              merkledb.BranchFactor16,
		HistoryLength:             HistoryLength,
		ValueNodeCacheSize:        valueNodeCacheSize,
		IntermediateNodeCacheSize: intermediateNodeCacheSize,
		Reg:                       prometheus.NewRegistry(),
		Tracer:                    noOpTracer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating merkleDB: %w", err)
	}

	txCache, err := metercacher.New(
		"tx_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txAndStatus](execCfg.TxCacheSize, txAndStatusSize),
	)
	if err != nil {
		return nil, err
	}

	rewardUTXOsCache, err := metercacher.New[ids.ID, []*avax.UTXO](
		"reward_utxos_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*avax.UTXO]{Size: execCfg.RewardUTXOsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	subnetOwnerCache, err := metercacher.New[ids.ID, fxOwnerAndSize](
		"subnet_owner_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, fxOwnerAndSize](execCfg.FxOwnerCacheSize, func(_ ids.ID, f fxOwnerAndSize) int {
			return ids.IDLen + f.size
		}),
	)
	if err != nil {
		return nil, err
	}

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

	blockCache, err := metercacher.New[ids.ID, block.Block](
		"block_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, block.Block](execCfg.BlockCacheSize, blockSize),
	)
	if err != nil {
		return nil, err
	}

	blockIDCache, err := metercacher.New[uint64, ids.ID](
		"block_id_cache",
		metricsReg,
		&cache.LRU[uint64, ids.ID]{Size: execCfg.BlockIDCacheSize},
	)
	if err != nil {
		return nil, err
	}

	return &state{
		validators: validators,
		ctx:        ctx,
		metrics:    metrics,
		rewards:    rewards,

		baseDB:       baseDB,
		singletonDB:  singletonDB,
		baseMerkleDB: baseMerkleDB,
		merkleDB:     merkleDB,

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		delegateeRewardCache:    make(map[ids.NodeID]map[ids.ID]uint64),
		modifiedDelegateeReward: make(map[ids.NodeID]set.Set[ids.ID]),

		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		utxoCache:     &cache.LRU[ids.ID, *avax.UTXO]{Size: utxoCacheSize},

		modifiedSupplies: make(map[ids.ID]uint64),
		suppliesCache:    supplyCache,

		subnetOwners:     make(map[ids.ID]fx.Owner),
		subnetOwnerCache: subnetOwnerCache,

		addedPermissionedSubnets: make([]*txs.Tx, 0),
		permissionedSubnetCache:  nil, // created first time GetSubnets is called
		addedElasticSubnets:      make(map[ids.ID]*txs.Tx),
		elasticSubnetCache:       transformedSubnetCache,

		addedChains: make(map[ids.ID][]*txs.Tx),
		chainCache:  chainCache,

		addedBlocks: make(map[ids.ID]block.Block),
		blockCache:  blockCache,
		blockDB:     blockDB,

		addedBlockIDs: make(map[uint64]ids.ID),
		blockIDCache:  blockIDCache,
		blockIDDB:     blockIDsDB,

		addedTxs: make(map[ids.ID]*txAndStatus),
		txCache:  txCache,
		txDB:     txDB,

		indexedUTXOsDB: indexedUTXOsDB,

		localUptimesCache:    make(map[ids.NodeID]map[ids.ID]*uptimes),
		modifiedLocalUptimes: make(map[ids.NodeID]set.Set[ids.ID]),
		localUptimesDB:       localUptimesDB,

		flatValidatorWeightDiffsDB:    flatValidatorWeightDiffsDB,
		flatValidatorPublicKeyDiffsDB: flatValidatorPublicKeyDiffsDB,

		addedRewardUTXOs: make(map[ids.ID][]*avax.UTXO),
		rewardUTXOsCache: rewardUTXOsCache,
		rewardUTXOsDB:    rewardUTXOsDB,
	}, nil
}

// STAKERS section
func (s *state) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.currentStakers.GetValidator(subnetID, nodeID)
}

func (s *state) PutCurrentValidator(staker *Staker) {
	s.currentStakers.PutValidator(staker)

	// make sure that each new validator has an uptime entry
	// and a delegatee reward entry. MerkleState implementations
	// of SetUptime and SetDelegateeReward must not err
	err := s.SetUptime(staker.NodeID, staker.SubnetID, 0 /*duration*/, staker.StartTime)
	if err != nil {
		panic(err)
	}
	err = s.SetDelegateeReward(staker.SubnetID, staker.NodeID, 0)
	if err != nil {
		panic(err)
	}
}

func (s *state) DeleteCurrentValidator(staker *Staker) {
	s.currentStakers.DeleteValidator(staker)
}

func (s *state) GetSubnets() ([]*txs.Tx, error) {
	// Note: we want all subnets, so we don't look at addedSubnets
	// which are only part of them
	if s.permissionedSubnetCache != nil {
		return s.permissionedSubnetCache, nil
	}

	subnetDBIt := s.merkleDB.NewIteratorWithPrefix(permissionedSubnetSectionPrefix)
	defer subnetDBIt.Release()

	subnets := make([]*txs.Tx, 0)
	for subnetDBIt.Next() {
		subnetTxBytes := subnetDBIt.Value()
		subnetTx, err := txs.Parse(txs.GenesisCodec, subnetTxBytes)
		if err != nil {
			return nil, err
		}
		subnets = append(subnets, subnetTx)
	}
	if err := subnetDBIt.Error(); err != nil {
		return nil, err
	}
	subnets = append(subnets, s.addedPermissionedSubnets...)
	s.permissionedSubnetCache = subnets
	return subnets, nil
}

func (s *state) AddSubnet(createSubnetTx *txs.Tx) {
	s.addedPermissionedSubnets = append(s.addedPermissionedSubnets, createSubnetTx)
}

func (s *state) GetSubnetOwner(subnetID ids.ID) (fx.Owner, error) {
	if owner, exists := s.subnetOwners[subnetID]; exists {
		return owner, nil
	}

	if ownerAndSize, cached := s.subnetOwnerCache.Get(subnetID); cached {
		if ownerAndSize.owner == nil {
			return nil, database.ErrNotFound
		}
		return ownerAndSize.owner, nil
	}

	subnetIDKey := merkleSubnetOwnersKey(subnetID)
	ownerBytes, err := s.merkleDB.Get(subnetIDKey)
	if err == nil {
		var owner fx.Owner
		if _, err := block.GenesisCodec.Unmarshal(ownerBytes, &owner); err != nil {
			return nil, err
		}
		s.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})
		return owner, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	subnetIntf, _, err := s.GetTx(subnetID)
	if err != nil {
		if err == database.ErrNotFound {
			s.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{})
		}
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%q %w", subnetID, errIsNotSubnet)
	}

	s.SetSubnetOwner(subnetID, subnet.Owner)
	return subnet.Owner, nil
}

func (s *state) SetSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	s.subnetOwners[subnetID] = owner
}

func (s *state) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	if tx, exists := s.addedElasticSubnets[subnetID]; exists {
		return tx, nil
	}

	if tx, cached := s.elasticSubnetCache.Get(subnetID); cached {
		if tx == nil {
			return nil, database.ErrNotFound
		}
		return tx, nil
	}

	key := merkleElasticSubnetKey(subnetID)
	transformSubnetTxBytes, err := s.merkleDB.Get(key)
	switch err {
	case nil:
		transformSubnetTx, err := txs.Parse(txs.GenesisCodec, transformSubnetTxBytes)
		if err != nil {
			return nil, err
		}
		s.elasticSubnetCache.Put(subnetID, transformSubnetTx)
		return transformSubnetTx, nil

	case database.ErrNotFound:
		s.elasticSubnetCache.Put(subnetID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (s *state) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	s.addedElasticSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
}

func (s *state) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	if chains, cached := s.chainCache.Get(subnetID); cached {
		return chains, nil
	}

	prefix := merkleChainPrefix(subnetID)
	chainDBIt := s.merkleDB.NewIteratorWithPrefix(prefix)
	defer chainDBIt.Release()

	chains := make([]*txs.Tx, 0)

	for chainDBIt.Next() {
		chainTxBytes := chainDBIt.Value()
		chainTx, err := txs.Parse(txs.GenesisCodec, chainTxBytes)
		if err != nil {
			return nil, err
		}
		chains = append(chains, chainTx)
	}
	if err := chainDBIt.Error(); err != nil {
		return nil, err
	}
	chains = append(chains, s.addedChains[subnetID]...)
	s.chainCache.Put(subnetID, chains)
	return chains, nil
}

func (s *state) AddChain(createChainTxIntf *txs.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*txs.CreateChainTx)
	subnetID := createChainTx.SubnetID

	s.addedChains[subnetID] = append(s.addedChains[subnetID], createChainTxIntf)
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
	switch err {
	case nil:
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

	case database.ErrNotFound:
		s.txCache.Put(txID, nil)
		return nil, status.Unknown, database.ErrNotFound

	default:
		return nil, status.Unknown, err
	}
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
		return utxos, nil
	}

	rawTxDB := prefixdb.New(txID[:], s.rewardUTXOsDB)
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
	if utxo, found := s.utxoCache.Get(utxoID); found {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}

	key := merkleUtxoIDKey(utxoID)

	switch bytes, err := s.merkleDB.Get(key); err {
	case nil:
		utxo := &avax.UTXO{}
		if _, err := txs.GenesisCodec.Unmarshal(bytes, utxo); err != nil {
			return nil, err
		}
		s.utxoCache.Put(utxoID, utxo)
		return utxo, nil

	case database.ErrNotFound:
		s.utxoCache.Put(utxoID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (s *state) UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	var (
		prefix = slices.Clone(addr)
		key    = merkleUtxoIndexKey(addr, start)
	)

	iter := s.indexedUTXOsDB.NewIteratorWithStartAndPrefix(key, prefix)
	defer iter.Release()

	utxoIDs := []ids.ID(nil)
	for len(utxoIDs) < limit && iter.Next() {
		itAddr, utxoID := splitUtxoIndexKey(iter.Key())
		if !bytes.Equal(itAddr, addr) {
			break
		}
		if utxoID == start {
			continue
		}

		start = ids.Empty
		utxoIDs = append(utxoIDs, utxoID)
	}
	return utxoIDs, iter.Error()
}

func (s *state) AddUTXO(utxo *avax.UTXO) {
	s.modifiedUTXOs[utxo.InputID()] = utxo
}

func (s *state) DeleteUTXO(utxoID ids.ID) {
	s.modifiedUTXOs[utxoID] = nil
}

func (s *state) GetStartTime(nodeID ids.NodeID, subnetID ids.ID) (time.Time, error) {
	staker, err := s.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return staker.StartTime, nil
}

func (s *state) GetTimestamp() time.Time {
	return s.chainTime
}

func (s *state) SetTimestamp(tm time.Time) {
	s.chainTime = tm
}

func (s *state) GetLastAccepted() ids.ID {
	return s.lastAcceptedBlkID
}

func (s *state) SetLastAccepted(lastAccepted ids.ID) {
	s.lastAcceptedBlkID = lastAccepted
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

func (s *state) shouldInit() (bool, error) {
	has, err := s.singletonDB.Has(initializedKey)
	return !has, err
}

func (s *state) doneInit() error {
	return s.singletonDB.Put(initializedKey, nil)
}

func (s *state) GetDelegateeReward(subnetID ids.ID, vdrID ids.NodeID) (uint64, error) {
	nodeDelegateeRewards, exists := s.delegateeRewardCache[vdrID]
	if exists {
		delegateeReward, exists := nodeDelegateeRewards[subnetID]
		if exists {
			return delegateeReward, nil
		}
	}

	// try loading from the db
	key := merkleDelegateeRewardsKey(vdrID, subnetID)
	amountBytes, err := s.merkleDB.Get(key)
	if err != nil {
		return 0, err
	}
	delegateeReward, err := database.ParseUInt64(amountBytes)
	if err != nil {
		return 0, err
	}

	if _, found := s.delegateeRewardCache[vdrID]; !found {
		s.delegateeRewardCache[vdrID] = make(map[ids.ID]uint64)
	}
	s.delegateeRewardCache[vdrID][subnetID] = delegateeReward
	return delegateeReward, nil
}

func (s *state) SetDelegateeReward(subnetID ids.ID, vdrID ids.NodeID, amount uint64) error {
	nodeDelegateeRewards, exists := s.delegateeRewardCache[vdrID]
	if !exists {
		nodeDelegateeRewards = make(map[ids.ID]uint64)
		s.delegateeRewardCache[vdrID] = nodeDelegateeRewards
	}
	nodeDelegateeRewards[subnetID] = amount

	// track diff
	updatedDelegateeRewards, ok := s.modifiedDelegateeReward[vdrID]
	if !ok {
		updatedDelegateeRewards = set.Set[ids.ID]{}
		s.modifiedDelegateeReward[vdrID] = updatedDelegateeRewards
	}
	updatedDelegateeRewards.Add(subnetID)
	return nil
}

// METADATA Section

func (s *state) SetHeight(height uint64) {
	s.lastAcceptedHeight = height
}

func (s *state) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	supply, ok := s.modifiedSupplies[subnetID]
	if ok {
		return supply, nil
	}
	cachedSupply, ok := s.suppliesCache.Get(subnetID)
	if ok {
		if cachedSupply == nil {
			return 0, database.ErrNotFound
		}
		return *cachedSupply, nil
	}

	key := merkleSuppliesKey(subnetID)

	switch supplyBytes, err := s.merkleDB.Get(key); err {
	case nil:
		supply, err := database.ParseUInt64(supplyBytes)
		if err != nil {
			return 0, fmt.Errorf("failed parsing supply: %w", err)
		}
		s.suppliesCache.Put(subnetID, &supply)
		return supply, nil

	case database.ErrNotFound:
		s.suppliesCache.Put(subnetID, nil)
		return 0, database.ErrNotFound

	default:
		return 0, err
	}
}

func (s *state) SetCurrentSupply(subnetID ids.ID, cs uint64) {
	s.modifiedSupplies[subnetID] = cs
}

// SUBNETS Section

// CHAINS Section

// TXs Section

// REWARD UTXOs SECTION

// BLOCKs Section
func (s *state) GetStatelessBlock(blockID ids.ID) (block.Block, error) {
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
	switch err {
	case nil:
		// Note: stored blocks are verified, so it's safe to unmarshal them with GenesisCodec
		blk, err := block.Parse(block.GenesisCodec, blkBytes)
		if err != nil {
			return nil, err
		}

		s.blockCache.Put(blockID, blk)
		return blk, nil

	case database.ErrNotFound:
		s.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (s *state) AddStatelessBlock(block block.Block) {
	s.addedBlocks[block.ID()] = block
}

func (s *state) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, exists := s.addedBlockIDs[height]; exists {
		return blkID, nil
	}
	if blkID, cached := s.blockIDCache.Get(height); cached {
		if blkID == ids.Empty {
			return ids.Empty, database.ErrNotFound
		}

		return blkID, nil
	}

	heightKey := database.PackUInt64(height)

	blkID, err := database.GetID(s.blockIDDB, heightKey)
	if err == database.ErrNotFound {
		s.blockIDCache.Put(height, ids.Empty)
		return ids.Empty, database.ErrNotFound
	}
	if err != nil {
		return ids.Empty, err
	}

	s.blockIDCache.Put(height, blkID)
	return blkID, nil
}

// UPTIMES SECTION
func (s *state) GetUptime(vdrID ids.NodeID, subnetID ids.ID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	nodeUptimes, exists := s.localUptimesCache[vdrID]
	if exists {
		uptime, exists := nodeUptimes[subnetID]
		if exists {
			return uptime.Duration, uptime.lastUpdated, nil
		}
	}

	// try loading from DB
	key := merkleLocalUptimesKey(vdrID, subnetID)
	uptimeBytes, err := s.localUptimesDB.Get(key)
	switch err {
	case nil:
		upTm := &uptimes{}
		if _, err := txs.GenesisCodec.Unmarshal(uptimeBytes, upTm); err != nil {
			return 0, time.Time{}, err
		}
		upTm.lastUpdated = time.Unix(int64(upTm.LastUpdated), 0)
		s.localUptimesCache[vdrID] = make(map[ids.ID]*uptimes)
		s.localUptimesCache[vdrID][subnetID] = upTm
		return upTm.Duration, upTm.lastUpdated, nil

	case database.ErrNotFound:
		// no local data for this staker uptime
		return 0, time.Time{}, database.ErrNotFound
	default:
		return 0, time.Time{}, err
	}
}

func (s *state) SetUptime(vdrID ids.NodeID, subnetID ids.ID, upDuration time.Duration, lastUpdated time.Time) error {
	nodeUptimes, exists := s.localUptimesCache[vdrID]
	if !exists {
		nodeUptimes = make(map[ids.ID]*uptimes)
		s.localUptimesCache[vdrID] = nodeUptimes
	}

	nodeUptimes[subnetID] = &uptimes{
		Duration:    upDuration,
		LastUpdated: uint64(lastUpdated.Unix()),
		lastUpdated: lastUpdated,
	}

	// track diff
	updatedNodeUptimes, ok := s.modifiedLocalUptimes[vdrID]
	if !ok {
		updatedNodeUptimes = set.Set[ids.ID]{}
		s.modifiedLocalUptimes[vdrID] = updatedNodeUptimes
	}
	updatedNodeUptimes.Add(subnetID)
	return nil
}

// UTXOs section

// VALIDATORS Section
func applyWeightDiff(
	vdrs map[ids.NodeID]*validators.GetValidatorOutput,
	nodeID ids.NodeID,
	weightDiff *ValidatorWeightDiff,
) error {
	vdr, ok := vdrs[nodeID]
	if !ok {
		// This node isn't in the current validator set.
		vdr = &validators.GetValidatorOutput{
			NodeID: nodeID,
		}
		vdrs[nodeID] = vdr
	}

	// The weight of this node changed at this block.
	var err error
	if weightDiff.Decrease {
		// The validator's weight was decreased at this block, so in the
		// prior block it was higher.
		vdr.Weight, err = safemath.Add64(vdr.Weight, weightDiff.Amount)
	} else {
		// The validator's weight was increased at this block, so in the
		// prior block it was lower.
		vdr.Weight, err = safemath.Sub(vdr.Weight, weightDiff.Amount)
	}
	if err != nil {
		return err
	}

	if vdr.Weight == 0 {
		// The validator's weight was 0 before this block so they weren't in the
		// validator set.
		delete(vdrs, nodeID)
	}
	return nil
}

func (s *state) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	diffIter := s.flatValidatorWeightDiffsDB.NewIteratorWithStartAndPrefix(
		marshalStartDiffKey(subnetID, startHeight),
		subnetID[:],
	)
	defer diffIter.Release()

	for diffIter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, parsedHeight, nodeID, err := unmarshalDiffKey(diffIter.Key())
		if err != nil {
			return err
		}
		// If the parsedHeight is less than our target endHeight, then we have
		// fully processed the diffs from startHeight through endHeight.
		if parsedHeight < endHeight {
			return diffIter.Error()
		}

		weightDiff, err := unmarshalWeightDiff(diffIter.Value())
		if err != nil {
			return err
		}

		if err := applyWeightDiff(validators, nodeID, weightDiff); err != nil {
			return err
		}
	}

	return diffIter.Error()
}

func (s *state) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
) error {
	diffIter := s.flatValidatorPublicKeyDiffsDB.NewIteratorWithStartAndPrefix(
		marshalStartDiffKey(constants.PrimaryNetworkID, startHeight),
		constants.PrimaryNetworkID[:],
	)
	defer diffIter.Release()

	for diffIter.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, parsedHeight, nodeID, err := unmarshalDiffKey(diffIter.Key())
		if err != nil {
			return err
		}
		// If the parsedHeight is less than our target endHeight, then we have
		// fully processed the diffs from startHeight through endHeight.
		if parsedHeight < endHeight {
			break
		}

		vdr, ok := validators[nodeID]
		if !ok {
			continue
		}

		pkBytes := diffIter.Value()
		if len(pkBytes) == 0 {
			vdr.PublicKey = nil
			continue
		}

		vdr.PublicKey = new(bls.PublicKey).Deserialize(pkBytes)
	}
	return diffIter.Error()
}

// DB Operations
func (s *state) Abort() {
	s.baseDB.Abort()
}

func (s *state) Commit() error {
	defer s.Abort()
	batch, err := s.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (s *state) CommitBatch() (database.Batch, error) {
	// updateValidators is set to true here so that the validator manager is
	// kept up to date with the last accepted state.
	if err := s.write(true /*updateValidators*/, s.lastAcceptedHeight); err != nil {
		return nil, err
	}
	return s.baseDB.CommitBatch()
}

func (*state) Checksum() ids.ID {
	return ids.Empty
}

func (s *state) Close() error {
	return utils.Err(
		s.flatValidatorWeightDiffsDB.Close(),
		s.flatValidatorPublicKeyDiffsDB.Close(),
		s.localUptimesDB.Close(),
		s.indexedUTXOsDB.Close(),
		s.txDB.Close(),
		s.blockDB.Close(),
		s.blockIDDB.Close(),
		s.merkleDB.Close(),
		s.baseMerkleDB.Close(),
	)
}

func (s *state) write(updateValidators bool, height uint64) error {
	currentData, weightDiffs, blsKeyDiffs, valSetDiff, err := s.processCurrentStakers()
	if err != nil {
		return err
	}
	pendingData, err := s.processPendingStakers()
	if err != nil {
		return err
	}

	return utils.Err(
		s.writeMerkleState(currentData, pendingData),
		s.writeBlocks(),
		s.writeTxs(),
		s.writeLocalUptimes(),
		s.writeWeightDiffs(height, weightDiffs),
		s.writeBlsKeyDiffs(height, blsKeyDiffs),
		s.writeRewardUTXOs(),
		s.updateValidatorSet(updateValidators, valSetDiff, weightDiffs),
	)
}

func (s *state) processCurrentStakers() (
	map[ids.ID]*stakersData,
	map[weightDiffKey]*ValidatorWeightDiff,
	map[ids.NodeID]*bls.PublicKey,
	map[weightDiffKey]*diffValidator,
	error,
) {
	var (
		outputStakers = make(map[ids.ID]*stakersData)
		outputWeights = make(map[weightDiffKey]*ValidatorWeightDiff)
		outputBlsKey  = make(map[ids.NodeID]*bls.PublicKey)
		outputValSet  = make(map[weightDiffKey]*diffValidator)
	)

	for subnetID, subnetValidatorDiffs := range s.currentStakers.validatorDiffs {
		delete(s.currentStakers.validatorDiffs, subnetID)
		for nodeID, validatorDiff := range subnetValidatorDiffs {
			weightKey := weightDiffKey{
				subnetID: subnetID,
				nodeID:   nodeID,
			}
			outputValSet[weightKey] = validatorDiff

			// make sure there is an entry for delegators even in case
			// there are no validators modified.
			outputWeights[weightKey] = &ValidatorWeightDiff{
				Decrease: validatorDiff.validatorStatus == deleted,
			}

			switch validatorDiff.validatorStatus {
			case added:
				var (
					txID            = validatorDiff.validator.TxID
					potentialReward = validatorDiff.validator.PotentialReward
					weight          = validatorDiff.validator.Weight
					blkKey          = validatorDiff.validator.PublicKey
				)
				tx, _, err := s.GetTx(txID)
				if err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed loading current validator tx, %w", err)
				}

				outputStakers[txID] = &stakersData{
					TxBytes:         tx.Bytes(),
					PotentialReward: potentialReward,
				}
				outputWeights[weightKey].Amount = weight

				if blkKey != nil {
					// Record that the public key for the validator is being
					// added. This means the prior value for the public key was
					// nil.
					outputBlsKey[nodeID] = nil
				}

			case deleted:
				var (
					txID   = validatorDiff.validator.TxID
					weight = validatorDiff.validator.Weight
					blkKey = validatorDiff.validator.PublicKey
				)

				outputStakers[txID] = &stakersData{
					TxBytes: nil,
				}
				outputWeights[weightKey].Amount = weight

				if blkKey != nil {
					// Record that the public key for the validator is being
					// removed. This means we must record the prior value of the
					// public key.
					outputBlsKey[nodeID] = blkKey
				}
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			defer addedDelegatorIterator.Release()
			for addedDelegatorIterator.Next() {
				staker := addedDelegatorIterator.Value()
				tx, _, err := s.GetTx(staker.TxID)
				if err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed loading current delegator tx, %w", err)
				}

				outputStakers[staker.TxID] = &stakersData{
					TxBytes:         tx.Bytes(),
					PotentialReward: staker.PotentialReward,
				}
				if err := outputWeights[weightKey].Add(false, staker.Weight); err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to increase node weight diff: %w", err)
				}
			}

			for _, staker := range validatorDiff.deletedDelegators {
				txID := staker.TxID

				outputStakers[txID] = &stakersData{
					TxBytes: nil,
				}
				if err := outputWeights[weightKey].Add(true, staker.Weight); err != nil {
					return nil, nil, nil, nil, fmt.Errorf("failed to decrease node weight diff: %w", err)
				}
			}
		}
	}
	return outputStakers, outputWeights, outputBlsKey, outputValSet, nil
}

func (s *state) processPendingStakers() (map[ids.ID]*stakersData, error) {
	output := make(map[ids.ID]*stakersData)
	for subnetID, subnetValidatorDiffs := range s.pendingStakers.validatorDiffs {
		delete(s.pendingStakers.validatorDiffs, subnetID)
		for _, validatorDiff := range subnetValidatorDiffs {
			// validatorDiff.validator is not guaranteed to be non-nil here.
			// Access it only if validatorDiff.validatorStatus is added or deleted
			switch validatorDiff.validatorStatus {
			case added:
				txID := validatorDiff.validator.TxID
				tx, _, err := s.GetTx(txID)
				if err != nil {
					return nil, fmt.Errorf("failed loading pending validator tx, %w", err)
				}
				output[txID] = &stakersData{
					TxBytes:         tx.Bytes(),
					PotentialReward: 0,
				}
			case deleted:
				txID := validatorDiff.validator.TxID
				output[txID] = &stakersData{
					TxBytes: nil,
				}
			}

			addedDelegatorIterator := NewTreeIterator(validatorDiff.addedDelegators)
			defer addedDelegatorIterator.Release()
			for addedDelegatorIterator.Next() {
				staker := addedDelegatorIterator.Value()
				tx, _, err := s.GetTx(staker.TxID)
				if err != nil {
					return nil, fmt.Errorf("failed loading pending delegator tx, %w", err)
				}
				output[staker.TxID] = &stakersData{
					TxBytes:         tx.Bytes(),
					PotentialReward: 0,
				}
			}

			for _, staker := range validatorDiff.deletedDelegators {
				txID := staker.TxID
				output[txID] = &stakersData{
					TxBytes: nil,
				}
			}
		}
	}
	return output, nil
}

func (s *state) writeMerkleState(currentData, pendingData map[ids.ID]*stakersData) error {
	batchOps := make([]database.BatchOp, 0)
	err := utils.Err(
		s.writeMetadata(&batchOps),
		s.writePermissionedSubnets(&batchOps),
		s.writeSubnetOwners(&batchOps),
		s.writeElasticSubnets(&batchOps),
		s.writeChains(&batchOps),
		s.writeCurrentStakers(&batchOps, currentData),
		s.writePendingStakers(&batchOps, pendingData),
		s.writeDelegateeRewards(&batchOps),
		s.writeUTXOs(&batchOps),
	)
	if err != nil {
		return err
	}

	if len(batchOps) == 0 {
		// nothing to commit
		return nil
	}

	view, err := s.merkleDB.NewView(context.TODO(), merkledb.ViewChanges{BatchOps: batchOps})
	if err != nil {
		return fmt.Errorf("failed creating merkleDB view: %w", err)
	}
	if err := view.CommitToDB(context.TODO()); err != nil {
		return fmt.Errorf("failed committing merkleDB view: %w", err)
	}
	return s.logMerkleRoot(len(batchOps) != 0)
}

func (s *state) writeMetadata(batchOps *[]database.BatchOp) error {
	if !s.chainTime.Equal(s.latestComittedChainTime) {
		encodedChainTime, err := s.chainTime.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to encoding chainTime: %w", err)
		}

		*batchOps = append(*batchOps, database.BatchOp{
			Key:   merkleChainTimeKey,
			Value: encodedChainTime,
		})
		s.latestComittedChainTime = s.chainTime
	}

	if s.lastAcceptedBlkID != s.latestCommittedLastAcceptedBlkID {
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   merkleLastAcceptedBlkIDKey,
			Value: s.lastAcceptedBlkID[:],
		})
		s.latestCommittedLastAcceptedBlkID = s.lastAcceptedBlkID
	}

	// lastAcceptedBlockHeight not persisted yet in merkleDB state.
	// TODO: Consider if it should be

	for subnetID, supply := range s.modifiedSupplies {
		supply := supply
		delete(s.modifiedSupplies, subnetID) // clear up s.supplies to avoid potential double commits
		s.suppliesCache.Put(subnetID, &supply)

		key := merkleSuppliesKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: database.PackUInt64(supply),
		})
	}
	return nil
}

func (s *state) writePermissionedSubnets(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for _, subnetTx := range s.addedPermissionedSubnets {
		key := merklePermissionedSubnetKey(subnetTx.ID())
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: subnetTx.Bytes(),
		})
	}
	s.addedPermissionedSubnets = make([]*txs.Tx, 0)
	return nil
}

func (s *state) writeSubnetOwners(batchOps *[]database.BatchOp) error {
	for subnetID, owner := range s.subnetOwners {
		owner := owner

		ownerBytes, err := block.GenesisCodec.Marshal(block.Version, &owner)
		if err != nil {
			return fmt.Errorf("failed to marshal subnet owner: %w", err)
		}

		s.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})

		key := merkleSubnetOwnersKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: ownerBytes,
		})
	}
	maps.Clear(s.subnetOwners)
	return nil
}

func (s *state) writeElasticSubnets(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for subnetID, transforkSubnetTx := range s.addedElasticSubnets {
		key := merkleElasticSubnetKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: transforkSubnetTx.Bytes(),
		})
		delete(s.addedElasticSubnets, subnetID)

		// Note: Evict is used rather than Put here because tx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.elasticSubnetCache.Evict(subnetID)
	}
	return nil
}

func (s *state) writeChains(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for subnetID, chains := range s.addedChains {
		for _, chainTx := range chains {
			key := merkleChainKey(subnetID, chainTx.ID())
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: chainTx.Bytes(),
			})
		}
		delete(s.addedChains, subnetID)
	}
	return nil
}

func (*state) writeCurrentStakers(batchOps *[]database.BatchOp, currentData map[ids.ID]*stakersData) error {
	for stakerTxID, data := range currentData {
		key := merkleCurrentStakersKey(stakerTxID)

		if data.TxBytes == nil {
			*batchOps = append(*batchOps, database.BatchOp{
				Key:    key,
				Delete: true,
			})
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize current stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: dataBytes,
		})
	}
	return nil
}

func (*state) writePendingStakers(batchOps *[]database.BatchOp, pendingData map[ids.ID]*stakersData) error {
	for stakerTxID, data := range pendingData {
		key := merklePendingStakersKey(stakerTxID)

		if data.TxBytes == nil {
			*batchOps = append(*batchOps, database.BatchOp{
				Key:    key,
				Delete: true,
			})
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize pending stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: dataBytes,
		})
	}
	return nil
}

func (s *state) writeUTXOs(batchOps *[]database.BatchOp) error {
	for utxoID, utxo := range s.modifiedUTXOs {
		delete(s.modifiedUTXOs, utxoID)
		key := merkleUtxoIDKey(utxoID)
		if utxo == nil { // delete the UTXO
			switch utxo, err := s.GetUTXO(utxoID); err {
			case nil:
				s.utxoCache.Put(utxoID, nil)
				*batchOps = append(*batchOps, database.BatchOp{
					Key:    key,
					Delete: true,
				})
				// store the index
				if err := s.writeUTXOsIndex(utxo, false /*insertUtxo*/); err != nil {
					return err
				}
				// go process next utxo
				continue

			case database.ErrNotFound:
				// trying to delete a non-existing utxo.
				continue

			default:
				return err
			}
		}

		// insert the UTXO
		utxoBytes, err := txs.GenesisCodec.Marshal(txs.Version, utxo)
		if err != nil {
			return err
		}
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: utxoBytes,
		})

		// store the index
		if err := s.writeUTXOsIndex(utxo, true /*insertUtxo*/); err != nil {
			return err
		}
	}
	return nil
}

func (s *state) writeDelegateeRewards(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for nodeID, nodeDelegateeRewards := range s.modifiedDelegateeReward {
		nodeDelegateeRewardsList := nodeDelegateeRewards.List()
		for _, subnetID := range nodeDelegateeRewardsList {
			delegateeReward := s.delegateeRewardCache[nodeID][subnetID]

			key := merkleDelegateeRewardsKey(nodeID, subnetID)
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: database.PackUInt64(delegateeReward),
			})
		}
		delete(s.modifiedDelegateeReward, nodeID)
	}
	return nil
}

func (s *state) writeBlocks() error {
	for blkID, blk := range s.addedBlocks {
		var (
			blkID     = blkID
			blkHeight = blk.Height()
		)

		delete(s.addedBlockIDs, blkHeight)
		s.blockIDCache.Put(blkHeight, blkID)
		if err := database.PutID(s.blockIDDB, database.PackUInt64(blkHeight), blkID); err != nil {
			return fmt.Errorf("failed to write block height index: %w", err)
		}

		delete(s.addedBlocks, blkID)
		// Note: Evict is used rather than Put here because blk may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		s.blockCache.Evict(blkID)

		if err := s.blockDB.Put(blkID[:], blk.Bytes()); err != nil {
			return fmt.Errorf("failed to write block %s: %w", blkID, err)
		}
	}
	return nil
}

func (s *state) writeTxs() error {
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

func (s *state) writeUTXOsIndex(utxo *avax.UTXO, insertUtxo bool) error {
	addressable, ok := utxo.Out.(avax.Addressable)
	if !ok {
		return nil
	}
	addresses := addressable.Addresses()

	for _, addr := range addresses {
		key := merkleUtxoIndexKey(addr, utxo.InputID())

		if insertUtxo {
			if err := s.indexedUTXOsDB.Put(key, nil); err != nil {
				return err
			}
		} else {
			if err := s.indexedUTXOsDB.Delete(key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *state) writeLocalUptimes() error {
	for vdrID, updatedSubnets := range s.modifiedLocalUptimes {
		for subnetID := range updatedSubnets {
			key := merkleLocalUptimesKey(vdrID, subnetID)

			uptimes := s.localUptimesCache[vdrID][subnetID]
			uptimeBytes, err := txs.GenesisCodec.Marshal(txs.Version, uptimes)
			if err != nil {
				return err
			}

			if err := s.localUptimesDB.Put(key, uptimeBytes); err != nil {
				return fmt.Errorf("failed to add local uptimes: %w", err)
			}
		}
		delete(s.modifiedLocalUptimes, vdrID)
	}
	return nil
}

func (s *state) writeWeightDiffs(height uint64, weightDiffs map[weightDiffKey]*ValidatorWeightDiff) error {
	for weightKey, weightDiff := range weightDiffs {
		if weightDiff.Amount == 0 {
			// No weight change to record; go to next validator.
			continue
		}

		key := marshalDiffKey(weightKey.subnetID, height, weightKey.nodeID)
		weightDiffBytes := marshalWeightDiff(weightDiff)
		if err := s.flatValidatorWeightDiffsDB.Put(key, weightDiffBytes); err != nil {
			return fmt.Errorf("failed to add weight diffs: %w", err)
		}
	}
	return nil
}

func (s *state) writeBlsKeyDiffs(height uint64, blsKeyDiffs map[ids.NodeID]*bls.PublicKey) error {
	for nodeID, blsKey := range blsKeyDiffs {
		key := marshalDiffKey(constants.PrimaryNetworkID, height, nodeID)
		blsKeyBytes := []byte{}
		if blsKey != nil {
			// Note: We store the uncompressed public key here as it is
			// significantly more efficient to parse when applying
			// diffs.
			blsKeyBytes = blsKey.Serialize()
		}
		if err := s.flatValidatorPublicKeyDiffsDB.Put(key, blsKeyBytes); err != nil {
			return fmt.Errorf("failed to add bls key diffs: %w", err)
		}
	}
	return nil
}

func (s *state) writeRewardUTXOs() error {
	for txID, utxos := range s.addedRewardUTXOs {
		delete(s.addedRewardUTXOs, txID)
		s.rewardUTXOsCache.Put(txID, utxos)
		rawTxDB := prefixdb.New(txID[:], s.rewardUTXOsDB)
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

func (s *state) updateValidatorSet(
	updateValidators bool,
	valSetDiff map[weightDiffKey]*diffValidator,
	weightDiffs map[weightDiffKey]*ValidatorWeightDiff,
) error {
	if !updateValidators {
		return nil
	}

	for weightKey, weightDiff := range weightDiffs {
		var (
			subnetID      = weightKey.subnetID
			nodeID        = weightKey.nodeID
			validatorDiff = valSetDiff[weightKey]
			err           error
		)

		if weightDiff.Amount == 0 {
			// No weight change to record; go to next validator.
			continue
		}

		if weightDiff.Decrease {
			err = s.validators.RemoveWeight(subnetID, nodeID, weightDiff.Amount)
		} else {
			if validatorDiff.validatorStatus == added {
				staker := validatorDiff.validator
				err = s.validators.AddStaker(
					subnetID,
					nodeID,
					staker.PublicKey,
					staker.TxID,
					weightDiff.Amount,
				)
			} else {
				err = s.validators.AddWeight(subnetID, nodeID, weightDiff.Amount)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to update validator weight: %w", err)
		}
	}

	s.metrics.SetLocalStake(s.validators.GetWeight(constants.PrimaryNetworkID, s.ctx.NodeID))
	totalWeight, err := s.validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get total weight: %w", err)
	}
	s.metrics.SetTotalStake(totalWeight)
	return nil
}

func (s *state) logMerkleRoot(hasChanges bool) error {
	// get current Height
	blk, err := s.GetStatelessBlock(s.GetLastAccepted())
	if err != nil {
		// may happen in tests. Let's just skip
		return nil
	}

	if !hasChanges {
		s.ctx.Log.Info("merkle root",
			zap.Uint64("height", blk.Height()),
			zap.Stringer("blkID", blk.ID()),
			zap.String("merkle root", "no changes to merkle state"),
		)
		return nil
	}

	view, err := s.merkleDB.NewView(context.TODO(), merkledb.ViewChanges{})
	if err != nil {
		return fmt.Errorf("failed creating merkleDB view: %w", err)
	}
	root, err := view.GetMerkleRoot(context.TODO())
	if err != nil {
		return fmt.Errorf("failed pulling merkle root: %w", err)
	}

	s.ctx.Log.Info("merkle root",
		zap.Uint64("height", blk.Height()),
		zap.Stringer("blkID", blk.ID()),
		zap.String("merkle root", root.String()),
	)
	return nil
}
