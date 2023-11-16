// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	HistoryLength = uint(256)

	valueNodeCacheSize        = 512 * units.MiB
	intermediateNodeCacheSize = 512 * units.MiB
	utxoCacheSize             = 8192 // from avax/utxo_state.go
)

var (
	_ State = (*merkleState)(nil)

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

func NewMerkleState(
	rawDB database.Database,
	genesisBytes []byte,
	metricsReg prometheus.Registerer,
	cfg *config.Config,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metrics metrics.Metrics,
	rewards reward.Calculator,
) (State, error) {
	res, err := newMerkleState(
		rawDB,
		metrics,
		cfg,
		execCfg,
		ctx,
		metricsReg,
		rewards,
	)
	if err != nil {
		return nil, err
	}

	if err := res.sync(genesisBytes); err != nil {
		// Drop any errors on close to return the first error
		_ = res.Close()
		return nil, err
	}

	return res, nil
}

func newMerkleState(
	rawDB database.Database,
	metrics metrics.Metrics,
	cfg *config.Config,
	execCfg *config.ExecutionConfig,
	ctx *snow.Context,
	metricsReg prometheus.Registerer,
	rewards reward.Calculator,
) (*merkleState, error) {
	var (
		baseDB                        = versiondb.New(rawDB)
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

	rewardUTXOsCache, err := metercacher.New[ids.ID, []*avax.UTXO](
		"reward_utxos_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*avax.UTXO]{Size: execCfg.RewardUTXOsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	suppliesCache, err := metercacher.New[ids.ID, *uint64](
		"supply_cache",
		metricsReg,
		&cache.LRU[ids.ID, *uint64]{Size: execCfg.ChainCacheSize},
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

	txCache, err := metercacher.New(
		"tx_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txAndStatus](execCfg.TxCacheSize, txAndStatusSize),
	)
	if err != nil {
		return nil, err
	}

	return &merkleState{
		cfg:     cfg,
		ctx:     ctx,
		metrics: metrics,
		rewards: rewards,

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
		suppliesCache:    suppliesCache,

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
type merkleState struct {
	cfg     *config.Config
	ctx     *snow.Context
	metrics metrics.Metrics
	rewards reward.Calculator

	baseDB       *versiondb.Database
	singletonDB  database.Database
	baseMerkleDB database.Database
	merkleDB     merkledb.MerkleDB // Stores merkleized state

	// stakers section (missing Delegatee piece)
	// TODO: Consider moving delegatee to UTXOs section
	currentStakers *baseStakers
	pendingStakers *baseStakers

	delegateeRewardCache    map[ids.NodeID]map[ids.ID]uint64
	modifiedDelegateeReward map[ids.NodeID]set.Set[ids.ID]

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

// STAKERS section
func (ms *merkleState) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return ms.currentStakers.GetValidator(subnetID, nodeID)
}

func (ms *merkleState) PutCurrentValidator(staker *Staker) {
	ms.currentStakers.PutValidator(staker)

	// make sure that each new validator has an uptime entry
	// and a delegatee reward entry. MerkleState implementations
	// of SetUptime and SetDelegateeReward must not err
	err := ms.SetUptime(staker.NodeID, staker.SubnetID, 0 /*duration*/, staker.StartTime)
	if err != nil {
		panic(err)
	}
	err = ms.SetDelegateeReward(staker.SubnetID, staker.NodeID, 0)
	if err != nil {
		panic(err)
	}
}

func (ms *merkleState) DeleteCurrentValidator(staker *Staker) {
	ms.currentStakers.DeleteValidator(staker)
}

func (ms *merkleState) GetCurrentDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return ms.currentStakers.GetDelegatorIterator(subnetID, nodeID), nil
}

func (ms *merkleState) PutCurrentDelegator(staker *Staker) {
	ms.currentStakers.PutDelegator(staker)
}

func (ms *merkleState) DeleteCurrentDelegator(staker *Staker) {
	ms.currentStakers.DeleteDelegator(staker)
}

func (ms *merkleState) GetCurrentStakerIterator() (StakerIterator, error) {
	return ms.currentStakers.GetStakerIterator(), nil
}

func (ms *merkleState) GetPendingValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return ms.pendingStakers.GetValidator(subnetID, nodeID)
}

func (ms *merkleState) PutPendingValidator(staker *Staker) {
	ms.pendingStakers.PutValidator(staker)
}

func (ms *merkleState) DeletePendingValidator(staker *Staker) {
	ms.pendingStakers.DeleteValidator(staker)
}

func (ms *merkleState) GetPendingDelegatorIterator(subnetID ids.ID, nodeID ids.NodeID) (StakerIterator, error) {
	return ms.pendingStakers.GetDelegatorIterator(subnetID, nodeID), nil
}

func (ms *merkleState) PutPendingDelegator(staker *Staker) {
	ms.pendingStakers.PutDelegator(staker)
}

func (ms *merkleState) DeletePendingDelegator(staker *Staker) {
	ms.pendingStakers.DeleteDelegator(staker)
}

func (ms *merkleState) GetPendingStakerIterator() (StakerIterator, error) {
	return ms.pendingStakers.GetStakerIterator(), nil
}

func (ms *merkleState) GetDelegateeReward(subnetID ids.ID, vdrID ids.NodeID) (uint64, error) {
	nodeDelegateeRewards, exists := ms.delegateeRewardCache[vdrID]
	if exists {
		delegateeReward, exists := nodeDelegateeRewards[subnetID]
		if exists {
			return delegateeReward, nil
		}
	}

	// try loading from the db
	key := merkleDelegateeRewardsKey(vdrID, subnetID)
	amountBytes, err := ms.merkleDB.Get(key)
	if err != nil {
		return 0, err
	}
	delegateeReward, err := database.ParseUInt64(amountBytes)
	if err != nil {
		return 0, err
	}

	if _, found := ms.delegateeRewardCache[vdrID]; !found {
		ms.delegateeRewardCache[vdrID] = make(map[ids.ID]uint64)
	}
	ms.delegateeRewardCache[vdrID][subnetID] = delegateeReward
	return delegateeReward, nil
}

func (ms *merkleState) SetDelegateeReward(subnetID ids.ID, vdrID ids.NodeID, amount uint64) error {
	nodeDelegateeRewards, exists := ms.delegateeRewardCache[vdrID]
	if !exists {
		nodeDelegateeRewards = make(map[ids.ID]uint64)
		ms.delegateeRewardCache[vdrID] = nodeDelegateeRewards
	}
	nodeDelegateeRewards[subnetID] = amount

	// track diff
	updatedDelegateeRewards, ok := ms.modifiedDelegateeReward[vdrID]
	if !ok {
		updatedDelegateeRewards = set.Set[ids.ID]{}
		ms.modifiedDelegateeReward[vdrID] = updatedDelegateeRewards
	}
	updatedDelegateeRewards.Add(subnetID)
	return nil
}

// UTXOs section
func (ms *merkleState) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := ms.modifiedUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	if utxo, found := ms.utxoCache.Get(utxoID); found {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}

	key := merkleUtxoIDKey(utxoID)

	switch bytes, err := ms.merkleDB.Get(key); err {
	case nil:
		utxo := &avax.UTXO{}
		if _, err := txs.GenesisCodec.Unmarshal(bytes, utxo); err != nil {
			return nil, err
		}
		ms.utxoCache.Put(utxoID, utxo)
		return utxo, nil

	case database.ErrNotFound:
		ms.utxoCache.Put(utxoID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (ms *merkleState) UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	var (
		prefix = slices.Clone(addr)
		key    = merkleUtxoIndexKey(addr, start)
	)

	iter := ms.indexedUTXOsDB.NewIteratorWithStartAndPrefix(key, prefix)
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

func (ms *merkleState) AddUTXO(utxo *avax.UTXO) {
	ms.modifiedUTXOs[utxo.InputID()] = utxo
}

func (ms *merkleState) DeleteUTXO(utxoID ids.ID) {
	ms.modifiedUTXOs[utxoID] = nil
}

// METADATA Section
func (ms *merkleState) GetTimestamp() time.Time {
	return ms.chainTime
}

func (ms *merkleState) SetTimestamp(tm time.Time) {
	ms.chainTime = tm
}

func (ms *merkleState) GetLastAccepted() ids.ID {
	return ms.lastAcceptedBlkID
}

func (ms *merkleState) SetLastAccepted(lastAccepted ids.ID) {
	ms.lastAcceptedBlkID = lastAccepted
}

func (ms *merkleState) SetHeight(height uint64) {
	ms.lastAcceptedHeight = height
}

func (ms *merkleState) GetCurrentSupply(subnetID ids.ID) (uint64, error) {
	supply, ok := ms.modifiedSupplies[subnetID]
	if ok {
		return supply, nil
	}
	cachedSupply, ok := ms.suppliesCache.Get(subnetID)
	if ok {
		if cachedSupply == nil {
			return 0, database.ErrNotFound
		}
		return *cachedSupply, nil
	}

	key := merkleSuppliesKey(subnetID)

	switch supplyBytes, err := ms.merkleDB.Get(key); err {
	case nil:
		supply, err := database.ParseUInt64(supplyBytes)
		if err != nil {
			return 0, fmt.Errorf("failed parsing supply: %w", err)
		}
		ms.suppliesCache.Put(subnetID, &supply)
		return supply, nil

	case database.ErrNotFound:
		ms.suppliesCache.Put(subnetID, nil)
		return 0, database.ErrNotFound

	default:
		return 0, err
	}
}

func (ms *merkleState) SetCurrentSupply(subnetID ids.ID, cs uint64) {
	ms.modifiedSupplies[subnetID] = cs
}

// SUBNETS Section
func (ms *merkleState) GetSubnets() ([]*txs.Tx, error) {
	// Note: we want all subnets, so we don't look at addedSubnets
	// which are only part of them
	if ms.permissionedSubnetCache != nil {
		return ms.permissionedSubnetCache, nil
	}

	subnets := make([]*txs.Tx, 0)
	subnetDBIt := ms.merkleDB.NewIteratorWithPrefix(permissionedSubnetSectionPrefix)
	defer subnetDBIt.Release()

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
	subnets = append(subnets, ms.addedPermissionedSubnets...)
	ms.permissionedSubnetCache = subnets
	return subnets, nil
}

func (ms *merkleState) AddSubnet(createSubnetTx *txs.Tx) {
	ms.addedPermissionedSubnets = append(ms.addedPermissionedSubnets, createSubnetTx)
}

func (ms *merkleState) GetSubnetOwner(subnetID ids.ID) (fx.Owner, error) {
	if owner, exists := ms.subnetOwners[subnetID]; exists {
		return owner, nil
	}

	if ownerAndSize, cached := ms.subnetOwnerCache.Get(subnetID); cached {
		if ownerAndSize.owner == nil {
			return nil, database.ErrNotFound
		}
		return ownerAndSize.owner, nil
	}

	subnetIDKey := merkleSubnetOwnersKey(subnetID)
	ownerBytes, err := ms.merkleDB.Get(subnetIDKey)
	if err == nil {
		var owner fx.Owner
		if _, err := block.GenesisCodec.Unmarshal(ownerBytes, &owner); err != nil {
			return nil, err
		}
		ms.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})
		return owner, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}

	subnetIntf, _, err := ms.GetTx(subnetID)
	if err != nil {
		if err == database.ErrNotFound {
			ms.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{})
		}
		return nil, err
	}

	subnet, ok := subnetIntf.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%q %w", subnetID, errIsNotSubnet)
	}

	ms.SetSubnetOwner(subnetID, subnet.Owner)
	return subnet.Owner, nil
}

func (ms *merkleState) SetSubnetOwner(subnetID ids.ID, owner fx.Owner) {
	ms.subnetOwners[subnetID] = owner
}

func (ms *merkleState) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	if tx, exists := ms.addedElasticSubnets[subnetID]; exists {
		return tx, nil
	}

	if tx, cached := ms.elasticSubnetCache.Get(subnetID); cached {
		if tx == nil {
			return nil, database.ErrNotFound
		}
		return tx, nil
	}

	key := merkleElasticSubnetKey(subnetID)
	transformSubnetTxBytes, err := ms.merkleDB.Get(key)
	switch err {
	case nil:
		transformSubnetTx, err := txs.Parse(txs.GenesisCodec, transformSubnetTxBytes)
		if err != nil {
			return nil, err
		}
		ms.elasticSubnetCache.Put(subnetID, transformSubnetTx)
		return transformSubnetTx, nil

	case database.ErrNotFound:
		ms.elasticSubnetCache.Put(subnetID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (ms *merkleState) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	ms.addedElasticSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
}

// CHAINS Section
func (ms *merkleState) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	if chains, cached := ms.chainCache.Get(subnetID); cached {
		return chains, nil
	}
	chains := make([]*txs.Tx, 0)

	prefix := merkleChainPrefix(subnetID)

	chainDBIt := ms.merkleDB.NewIteratorWithPrefix(prefix)
	defer chainDBIt.Release()
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
	chains = append(chains, ms.addedChains[subnetID]...)
	ms.chainCache.Put(subnetID, chains)
	return chains, nil
}

func (ms *merkleState) AddChain(createChainTxIntf *txs.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*txs.CreateChainTx)
	subnetID := createChainTx.SubnetID

	ms.addedChains[subnetID] = append(ms.addedChains[subnetID], createChainTxIntf)
}

// TXs Section
func (ms *merkleState) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := ms.addedTxs[txID]; exists {
		return tx.tx, tx.status, nil
	}
	if tx, cached := ms.txCache.Get(txID); cached {
		if tx == nil {
			return nil, status.Unknown, database.ErrNotFound
		}
		return tx.tx, tx.status, nil
	}

	txBytes, err := ms.txDB.Get(txID[:])
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

		ms.txCache.Put(txID, ptx)
		return ptx.tx, ptx.status, nil

	case database.ErrNotFound:
		ms.txCache.Put(txID, nil)
		return nil, status.Unknown, database.ErrNotFound

	default:
		return nil, status.Unknown, err
	}
}

func (ms *merkleState) AddTx(tx *txs.Tx, status status.Status) {
	ms.addedTxs[tx.ID()] = &txAndStatus{
		tx:     tx,
		status: status,
	}
}

// BLOCKs Section
func (ms *merkleState) GetStatelessBlock(blockID ids.ID) (block.Block, error) {
	if blk, exists := ms.addedBlocks[blockID]; exists {
		return blk, nil
	}

	if blk, cached := ms.blockCache.Get(blockID); cached {
		if blk == nil {
			return nil, database.ErrNotFound
		}

		return blk, nil
	}

	blkBytes, err := ms.blockDB.Get(blockID[:])
	switch err {
	case nil:
		// Note: stored blocks are verified, so it's safe to unmarshal them with GenesisCodec
		blk, err := block.Parse(block.GenesisCodec, blkBytes)
		if err != nil {
			return nil, err
		}

		ms.blockCache.Put(blockID, blk)
		return blk, nil

	case database.ErrNotFound:
		ms.blockCache.Put(blockID, nil)
		return nil, database.ErrNotFound

	default:
		return nil, err
	}
}

func (ms *merkleState) AddStatelessBlock(block block.Block) {
	ms.addedBlocks[block.ID()] = block
}

func (ms *merkleState) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, exists := ms.addedBlockIDs[height]; exists {
		return blkID, nil
	}
	if blkID, cached := ms.blockIDCache.Get(height); cached {
		if blkID == ids.Empty {
			return ids.Empty, database.ErrNotFound
		}

		return blkID, nil
	}

	heightKey := database.PackUInt64(height)

	blkID, err := database.GetID(ms.blockIDDB, heightKey)
	if err == database.ErrNotFound {
		ms.blockIDCache.Put(height, ids.Empty)
		return ids.Empty, database.ErrNotFound
	}
	if err != nil {
		return ids.Empty, err
	}

	ms.blockIDCache.Put(height, blkID)
	return blkID, nil
}

func (*merkleState) ShouldPrune() (bool, error) {
	return false, nil // Nothing to do
}

func (*merkleState) PruneAndIndex(sync.Locker, logging.Logger) error {
	return nil // Nothing to do
}

// UPTIMES SECTION
func (ms *merkleState) GetUptime(vdrID ids.NodeID, subnetID ids.ID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	nodeUptimes, exists := ms.localUptimesCache[vdrID]
	if exists {
		uptime, exists := nodeUptimes[subnetID]
		if exists {
			return uptime.Duration, uptime.lastUpdated, nil
		}
	}

	// try loading from DB
	key := merkleLocalUptimesKey(vdrID, subnetID)
	uptimeBytes, err := ms.localUptimesDB.Get(key)
	switch err {
	case nil:
		upTm := &uptimes{}
		if _, err := txs.GenesisCodec.Unmarshal(uptimeBytes, upTm); err != nil {
			return 0, time.Time{}, err
		}
		upTm.lastUpdated = time.Unix(int64(upTm.LastUpdated), 0)
		ms.localUptimesCache[vdrID] = make(map[ids.ID]*uptimes)
		ms.localUptimesCache[vdrID][subnetID] = upTm
		return upTm.Duration, upTm.lastUpdated, nil

	case database.ErrNotFound:
		// no local data for this staker uptime
		return 0, time.Time{}, database.ErrNotFound
	default:
		return 0, time.Time{}, err
	}
}

func (ms *merkleState) SetUptime(vdrID ids.NodeID, subnetID ids.ID, upDuration time.Duration, lastUpdated time.Time) error {
	nodeUptimes, exists := ms.localUptimesCache[vdrID]
	if !exists {
		nodeUptimes = make(map[ids.ID]*uptimes)
		ms.localUptimesCache[vdrID] = nodeUptimes
	}

	nodeUptimes[subnetID] = &uptimes{
		Duration:    upDuration,
		LastUpdated: uint64(lastUpdated.Unix()),
		lastUpdated: lastUpdated,
	}

	// track diff
	updatedNodeUptimes, ok := ms.modifiedLocalUptimes[vdrID]
	if !ok {
		updatedNodeUptimes = set.Set[ids.ID]{}
		ms.modifiedLocalUptimes[vdrID] = updatedNodeUptimes
	}
	updatedNodeUptimes.Add(subnetID)
	return nil
}

func (ms *merkleState) GetStartTime(nodeID ids.NodeID, subnetID ids.ID) (time.Time, error) {
	staker, err := ms.GetCurrentValidator(subnetID, nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return staker.StartTime, nil
}

// REWARD UTXOs SECTION
func (ms *merkleState) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := ms.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	if utxos, exists := ms.rewardUTXOsCache.Get(txID); exists {
		return utxos, nil
	}

	rawTxDB := prefixdb.New(txID[:], ms.rewardUTXOsDB)
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

	ms.rewardUTXOsCache.Put(txID, utxos)
	return utxos, nil
}

func (ms *merkleState) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	ms.addedRewardUTXOs[txID] = append(ms.addedRewardUTXOs[txID], utxo)
}

// VALIDATORS Section
func (ms *merkleState) ApplyValidatorWeightDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
	subnetID ids.ID,
) error {
	diffIter := ms.flatValidatorWeightDiffsDB.NewIteratorWithStartAndPrefix(
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

func (ms *merkleState) ApplyValidatorPublicKeyDiffs(
	ctx context.Context,
	validators map[ids.NodeID]*validators.GetValidatorOutput,
	startHeight uint64,
	endHeight uint64,
) error {
	diffIter := ms.flatValidatorPublicKeyDiffsDB.NewIteratorWithStartAndPrefix(
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
func (ms *merkleState) Abort() {
	ms.baseDB.Abort()
}

func (ms *merkleState) Commit() error {
	defer ms.Abort()
	batch, err := ms.CommitBatch()
	if err != nil {
		return err
	}
	return batch.Write()
}

func (ms *merkleState) CommitBatch() (database.Batch, error) {
	// updateValidators is set to true here so that the validator manager is
	// kept up to date with the last accepted state.
	if err := ms.write(true /*updateValidators*/, ms.lastAcceptedHeight); err != nil {
		return nil, err
	}
	return ms.baseDB.CommitBatch()
}

func (*merkleState) Checksum() ids.ID {
	return ids.Empty
}

func (ms *merkleState) Close() error {
	return utils.Err(
		ms.flatValidatorWeightDiffsDB.Close(),
		ms.flatValidatorPublicKeyDiffsDB.Close(),
		ms.localUptimesDB.Close(),
		ms.indexedUTXOsDB.Close(),
		ms.txDB.Close(),
		ms.blockDB.Close(),
		ms.blockIDDB.Close(),
		ms.merkleDB.Close(),
		ms.baseMerkleDB.Close(),
	)
}

func (ms *merkleState) write(updateValidators bool, height uint64) error {
	currentData, weightDiffs, blsKeyDiffs, valSetDiff, err := ms.processCurrentStakers()
	if err != nil {
		return err
	}
	pendingData, err := ms.processPendingStakers()
	if err != nil {
		return err
	}

	return utils.Err(
		ms.writeMerkleState(currentData, pendingData),
		ms.writeBlocks(),
		ms.writeTxs(),
		ms.writeLocalUptimes(),
		ms.writeWeightDiffs(height, weightDiffs),
		ms.writeBlsKeyDiffs(height, blsKeyDiffs),
		ms.writeRewardUTXOs(),
		ms.updateValidatorSet(updateValidators, valSetDiff, weightDiffs),
	)
}

func (ms *merkleState) processCurrentStakers() (
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

	for subnetID, subnetValidatorDiffs := range ms.currentStakers.validatorDiffs {
		delete(ms.currentStakers.validatorDiffs, subnetID)
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
				tx, _, err := ms.GetTx(txID)
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
				tx, _, err := ms.GetTx(staker.TxID)
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

func (ms *merkleState) processPendingStakers() (map[ids.ID]*stakersData, error) {
	output := make(map[ids.ID]*stakersData)
	for subnetID, subnetValidatorDiffs := range ms.pendingStakers.validatorDiffs {
		delete(ms.pendingStakers.validatorDiffs, subnetID)
		for _, validatorDiff := range subnetValidatorDiffs {
			// validatorDiff.validator is not guaranteed to be non-nil here.
			// Access it only if validatorDiff.validatorStatus is added or deleted
			switch validatorDiff.validatorStatus {
			case added:
				txID := validatorDiff.validator.TxID
				tx, _, err := ms.GetTx(txID)
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
				tx, _, err := ms.GetTx(staker.TxID)
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

func (ms *merkleState) writeMerkleState(currentData, pendingData map[ids.ID]*stakersData) error {
	batchOps := make([]database.BatchOp, 0)
	err := utils.Err(
		ms.writeMetadata(&batchOps),
		ms.writePermissionedSubnets(&batchOps),
		ms.writeSubnetOwners(&batchOps),
		ms.writeElasticSubnets(&batchOps),
		ms.writeChains(&batchOps),
		ms.writeCurrentStakers(&batchOps, currentData),
		ms.writePendingStakers(&batchOps, pendingData),
		ms.writeDelegateeRewards(&batchOps),
		ms.writeUTXOs(&batchOps),
	)
	if err != nil {
		return err
	}

	if len(batchOps) == 0 {
		// nothing to commit
		return nil
	}

	view, err := ms.merkleDB.NewView(context.TODO(), merkledb.ViewChanges{BatchOps: batchOps})
	if err != nil {
		return fmt.Errorf("failed creating merkleDB view: %w", err)
	}
	if err := view.CommitToDB(context.TODO()); err != nil {
		return fmt.Errorf("failed committing merkleDB view: %w", err)
	}
	return ms.logMerkleRoot(len(batchOps) != 0)
}

func (ms *merkleState) writeMetadata(batchOps *[]database.BatchOp) error {
	if !ms.chainTime.Equal(ms.latestComittedChainTime) {
		encodedChainTime, err := ms.chainTime.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to encoding chainTime: %w", err)
		}

		*batchOps = append(*batchOps, database.BatchOp{
			Key:   merkleChainTimeKey,
			Value: encodedChainTime,
		})
		ms.latestComittedChainTime = ms.chainTime
	}

	if ms.lastAcceptedBlkID != ms.latestCommittedLastAcceptedBlkID {
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   merkleLastAcceptedBlkIDKey,
			Value: ms.lastAcceptedBlkID[:],
		})
		ms.latestCommittedLastAcceptedBlkID = ms.lastAcceptedBlkID
	}

	// lastAcceptedBlockHeight not persisted yet in merkleDB state.
	// TODO: Consider if it should be

	for subnetID, supply := range ms.modifiedSupplies {
		supply := supply
		delete(ms.modifiedSupplies, subnetID) // clear up ms.supplies to avoid potential double commits
		ms.suppliesCache.Put(subnetID, &supply)

		key := merkleSuppliesKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: database.PackUInt64(supply),
		})
	}
	return nil
}

func (ms *merkleState) writePermissionedSubnets(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for _, subnetTx := range ms.addedPermissionedSubnets {
		key := merklePermissionedSubnetKey(subnetTx.ID())
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: subnetTx.Bytes(),
		})
	}
	ms.addedPermissionedSubnets = make([]*txs.Tx, 0)
	return nil
}

func (ms *merkleState) writeSubnetOwners(batchOps *[]database.BatchOp) error {
	for subnetID, owner := range ms.subnetOwners {
		owner := owner

		ownerBytes, err := block.GenesisCodec.Marshal(block.Version, &owner)
		if err != nil {
			return fmt.Errorf("failed to marshal subnet owner: %w", err)
		}

		ms.subnetOwnerCache.Put(subnetID, fxOwnerAndSize{
			owner: owner,
			size:  len(ownerBytes),
		})

		key := merkleSubnetOwnersKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: ownerBytes,
		})
	}
	maps.Clear(ms.subnetOwners)
	return nil
}

func (ms *merkleState) writeElasticSubnets(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for subnetID, transforkSubnetTx := range ms.addedElasticSubnets {
		key := merkleElasticSubnetKey(subnetID)
		*batchOps = append(*batchOps, database.BatchOp{
			Key:   key,
			Value: transforkSubnetTx.Bytes(),
		})
		delete(ms.addedElasticSubnets, subnetID)

		// Note: Evict is used rather than Put here because tx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		ms.elasticSubnetCache.Evict(subnetID)
	}
	return nil
}

func (ms *merkleState) writeChains(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for subnetID, chains := range ms.addedChains {
		for _, chainTx := range chains {
			key := merkleChainKey(subnetID, chainTx.ID())
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: chainTx.Bytes(),
			})
		}
		delete(ms.addedChains, subnetID)
	}
	return nil
}

func (*merkleState) writeCurrentStakers(batchOps *[]database.BatchOp, currentData map[ids.ID]*stakersData) error {
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

func (*merkleState) writePendingStakers(batchOps *[]database.BatchOp, pendingData map[ids.ID]*stakersData) error {
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

func (ms *merkleState) writeUTXOs(batchOps *[]database.BatchOp) error {
	for utxoID, utxo := range ms.modifiedUTXOs {
		delete(ms.modifiedUTXOs, utxoID)
		key := merkleUtxoIDKey(utxoID)
		if utxo == nil { // delete the UTXO
			switch utxo, err := ms.GetUTXO(utxoID); err {
			case nil:
				ms.utxoCache.Put(utxoID, nil)
				*batchOps = append(*batchOps, database.BatchOp{
					Key:    key,
					Delete: true,
				})
				// store the index
				if err := ms.writeUTXOsIndex(utxo, false /*insertUtxo*/); err != nil {
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
		if err := ms.writeUTXOsIndex(utxo, true /*insertUtxo*/); err != nil {
			return err
		}
	}
	return nil
}

func (ms *merkleState) writeDelegateeRewards(batchOps *[]database.BatchOp) error { //nolint:golint,unparam
	for nodeID, nodeDelegateeRewards := range ms.modifiedDelegateeReward {
		nodeDelegateeRewardsList := nodeDelegateeRewards.List()
		for _, subnetID := range nodeDelegateeRewardsList {
			delegateeReward := ms.delegateeRewardCache[nodeID][subnetID]

			key := merkleDelegateeRewardsKey(nodeID, subnetID)
			*batchOps = append(*batchOps, database.BatchOp{
				Key:   key,
				Value: database.PackUInt64(delegateeReward),
			})
		}
		delete(ms.modifiedDelegateeReward, nodeID)
	}
	return nil
}

func (ms *merkleState) writeBlocks() error {
	for blkID, blk := range ms.addedBlocks {
		var (
			blkID     = blkID
			blkHeight = blk.Height()
		)

		delete(ms.addedBlockIDs, blkHeight)
		ms.blockIDCache.Put(blkHeight, blkID)
		if err := database.PutID(ms.blockIDDB, database.PackUInt64(blkHeight), blkID); err != nil {
			return fmt.Errorf("failed to write block height index: %w", err)
		}

		delete(ms.addedBlocks, blkID)
		// Note: Evict is used rather than Put here because blk may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		ms.blockCache.Evict(blkID)

		if err := ms.blockDB.Put(blkID[:], blk.Bytes()); err != nil {
			return fmt.Errorf("failed to write block %s: %w", blkID, err)
		}
	}
	return nil
}

func (ms *merkleState) writeTxs() error {
	for txID, txStatus := range ms.addedTxs {
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

		delete(ms.addedTxs, txID)
		// Note: Evict is used rather than Put here because stx may end up
		// referencing additional data (because of shared byte slices) that
		// would not be properly accounted for in the cache sizing.
		ms.txCache.Evict(txID)
		if err := ms.txDB.Put(txID[:], txBytes); err != nil {
			return fmt.Errorf("failed to add tx: %w", err)
		}
	}
	return nil
}

func (ms *merkleState) writeUTXOsIndex(utxo *avax.UTXO, insertUtxo bool) error {
	addressable, ok := utxo.Out.(avax.Addressable)
	if !ok {
		return nil
	}
	addresses := addressable.Addresses()

	for _, addr := range addresses {
		key := merkleUtxoIndexKey(addr, utxo.InputID())

		if insertUtxo {
			if err := ms.indexedUTXOsDB.Put(key, nil); err != nil {
				return err
			}
		} else {
			if err := ms.indexedUTXOsDB.Delete(key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ms *merkleState) writeLocalUptimes() error {
	for vdrID, updatedSubnets := range ms.modifiedLocalUptimes {
		for subnetID := range updatedSubnets {
			key := merkleLocalUptimesKey(vdrID, subnetID)

			uptimes := ms.localUptimesCache[vdrID][subnetID]
			uptimeBytes, err := txs.GenesisCodec.Marshal(txs.Version, uptimes)
			if err != nil {
				return err
			}

			if err := ms.localUptimesDB.Put(key, uptimeBytes); err != nil {
				return fmt.Errorf("failed to add local uptimes: %w", err)
			}
		}
		delete(ms.modifiedLocalUptimes, vdrID)
	}
	return nil
}

func (ms *merkleState) writeWeightDiffs(height uint64, weightDiffs map[weightDiffKey]*ValidatorWeightDiff) error {
	for weightKey, weightDiff := range weightDiffs {
		if weightDiff.Amount == 0 {
			// No weight change to record; go to next validator.
			continue
		}

		key := marshalDiffKey(weightKey.subnetID, height, weightKey.nodeID)
		weightDiffBytes := marshalWeightDiff(weightDiff)
		if err := ms.flatValidatorWeightDiffsDB.Put(key, weightDiffBytes); err != nil {
			return fmt.Errorf("failed to add weight diffs: %w", err)
		}
	}
	return nil
}

func (ms *merkleState) writeBlsKeyDiffs(height uint64, blsKeyDiffs map[ids.NodeID]*bls.PublicKey) error {
	for nodeID, blsKey := range blsKeyDiffs {
		key := marshalDiffKey(constants.PrimaryNetworkID, height, nodeID)
		blsKeyBytes := []byte{}
		if blsKey != nil {
			// Note: We store the uncompressed public key here as it is
			// significantly more efficient to parse when applying
			// diffs.
			blsKeyBytes = blsKey.Serialize()
		}
		if err := ms.flatValidatorPublicKeyDiffsDB.Put(key, blsKeyBytes); err != nil {
			return fmt.Errorf("failed to add bls key diffs: %w", err)
		}
	}
	return nil
}

func (ms *merkleState) writeRewardUTXOs() error {
	for txID, utxos := range ms.addedRewardUTXOs {
		delete(ms.addedRewardUTXOs, txID)
		ms.rewardUTXOsCache.Put(txID, utxos)
		rawTxDB := prefixdb.New(txID[:], ms.rewardUTXOsDB)
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

func (ms *merkleState) updateValidatorSet(
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
			err = ms.cfg.Validators.RemoveWeight(subnetID, nodeID, weightDiff.Amount)
		} else {
			if validatorDiff.validatorStatus == added {
				staker := validatorDiff.validator
				err = ms.cfg.Validators.AddStaker(
					subnetID,
					nodeID,
					staker.PublicKey,
					staker.TxID,
					weightDiff.Amount,
				)
			} else {
				err = ms.cfg.Validators.AddWeight(subnetID, nodeID, weightDiff.Amount)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to update validator weight: %w", err)
		}
	}

	ms.metrics.SetLocalStake(ms.cfg.Validators.GetWeight(constants.PrimaryNetworkID, ms.ctx.NodeID))
	totalWeight, err := ms.cfg.Validators.TotalWeight(constants.PrimaryNetworkID)
	if err != nil {
		return fmt.Errorf("failed to get total weight: %w", err)
	}
	ms.metrics.SetTotalStake(totalWeight)
	return nil
}

func (ms *merkleState) logMerkleRoot(hasChanges bool) error {
	// get current Height
	blk, err := ms.GetStatelessBlock(ms.GetLastAccepted())
	if err != nil {
		// may happen in tests. Let's just skip
		return nil
	}

	if !hasChanges {
		ms.ctx.Log.Info("merkle root",
			zap.Uint64("height", blk.Height()),
			zap.Stringer("blkID", blk.ID()),
			zap.String("merkle root", "no changes to merkle state"),
		)
		return nil
	}

	view, err := ms.merkleDB.NewView(context.TODO(), merkledb.ViewChanges{})
	if err != nil {
		return fmt.Errorf("failed creating merkleDB view: %w", err)
	}
	root, err := view.GetMerkleRoot(context.TODO())
	if err != nil {
		return fmt.Errorf("failed pulling merkle root: %w", err)
	}

	ms.ctx.Log.Info("merkle root",
		zap.Uint64("height", blk.Height()),
		zap.Stringer("blkID", blk.ID()),
		zap.String("merkle root", root.String()),
	)
	return nil
}
