// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	HistoryLength = int(256)    // from HyperSDK
	NodeCacheSize = int(65_536) // from HyperSDK

	utxoCacheSize = 8192 // from avax/utxo_state.go
)

var (
	_ State = (*merkleState)(nil)

	merkleStatePrefix      = []byte{0x0}
	merkleSingletonPrefix  = []byte{0x1}
	merkleBlockPrefix      = []byte{0x2}
	merkleTxPrefix         = []byte{0x3}
	merkleIndexUTXOsPrefix = []byte{0x4} // to serve UTXOIDs(addr)
	merkleUptimesPrefix    = []byte{0x5} // locally measured uptimes
	merkleWeightDiffPrefix = []byte{0x6} // non-merklelized validators weight diff. TODO: should we merklelize them?
	merkleBlsKeyDiffPrefix = []byte{0x7}

	// merkle db sections
	metadataSectionPrefix      = []byte{0x0}
	merkleChainTimeKey         = append(metadataSectionPrefix, []byte{0x0}...)
	merkleLastAcceptedBlkIDKey = append(metadataSectionPrefix, []byte{0x1}...)
	merkleSuppliesPrefix       = append(metadataSectionPrefix, []byte{0x2}...)

	permissionedSubnetSectionPrefix = []byte{0x1}
	elasticSubnetSectionPrefix      = []byte{0x2}
	chainsSectionPrefix             = []byte{0x3}
	utxosSectionPrefix              = []byte{0x4}
	rewardUtxosSectionPrefix        = []byte{0x5}
	currentStakersSectionPrefix     = []byte{0x6}
	pendingStakersSectionPrefix     = []byte{0x7}
	delegateeRewardsPrefix          = []byte{0x8}
)

func NewMerkleState(
	rawDB database.Database,
	metrics metrics.Metrics,
	genesisBytes []byte,
	cfg *config.Config,
	ctx *snow.Context,
	metricsReg prometheus.Registerer,
	rewards reward.Calculator,
	bootstrapped *utils.Atomic[bool],
) (State, error) {
	var (
		baseDB            = versiondb.New(rawDB)
		baseMerkleDB      = prefixdb.New(merkleStatePrefix, baseDB)
		singletonDB       = prefixdb.New(merkleSingletonPrefix, baseDB)
		blockDB           = prefixdb.New(merkleBlockPrefix, baseDB)
		txDB              = prefixdb.New(merkleTxPrefix, baseDB)
		indexedUTXOsDB    = prefixdb.New(merkleIndexUTXOsPrefix, baseDB)
		localUptimesDB    = prefixdb.New(merkleUptimesPrefix, baseDB)
		localWeightDiffDB = prefixdb.New(merkleWeightDiffPrefix, baseDB)
		localBlsKeyDiffDB = prefixdb.New(merkleBlsKeyDiffPrefix, baseDB)
	)

	traceCtx := context.TODO()
	noOpTracer, err := trace.New(trace.Config{Enabled: false})
	if err != nil {
		return nil, fmt.Errorf("failed creating noOpTraces: %w", err)
	}

	merkleDB, err := merkledb.New(traceCtx, baseMerkleDB, merkledb.Config{
		HistoryLength: HistoryLength,
		NodeCacheSize: NodeCacheSize,
		Reg:           prometheus.NewRegistry(),
		Tracer:        noOpTracer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating merkleDB: %w", err)
	}

	rewardUTXOsCache, err := metercacher.New[ids.ID, []*avax.UTXO](
		"reward_utxos_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*avax.UTXO]{Size: rewardUTXOsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	suppliesCache, err := metercacher.New[ids.ID, *uint64](
		"supply_cache",
		metricsReg,
		&cache.LRU[ids.ID, *uint64]{Size: chainCacheSize},
	)
	if err != nil {
		return nil, err
	}

	transformedSubnetCache, err := metercacher.New(
		"transformed_subnet_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txs.Tx](transformedSubnetTxCacheSize, txSize),
	)
	if err != nil {
		return nil, err
	}

	chainCache, err := metercacher.New[ids.ID, []*txs.Tx](
		"chain_cache",
		metricsReg,
		&cache.LRU[ids.ID, []*txs.Tx]{Size: chainCacheSize},
	)
	if err != nil {
		return nil, err
	}

	blockCache, err := metercacher.New[ids.ID, blocks.Block](
		"block_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, blocks.Block](blockCacheSize, blockSize),
	)
	if err != nil {
		return nil, err
	}

	txCache, err := metercacher.New(
		"tx_cache",
		metricsReg,
		cache.NewSizedLRU[ids.ID, *txAndStatus](txCacheSize, txAndStatusSize),
	)
	if err != nil {
		return nil, err
	}

	validatorWeightDiffsCache, err := metercacher.New[heightWithSubnet, map[ids.NodeID]*ValidatorWeightDiff](
		"validator_weight_diffs_cache",
		metricsReg,
		&cache.LRU[heightWithSubnet, map[ids.NodeID]*ValidatorWeightDiff]{Size: validatorDiffsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	validatorBlsKeyDiffsCache, err := metercacher.New[uint64, map[ids.NodeID]*bls.PublicKey](
		"validator_pub_key_diffs_cache",
		metricsReg,
		&cache.LRU[uint64, map[ids.NodeID]*bls.PublicKey]{Size: validatorDiffsCacheSize},
	)
	if err != nil {
		return nil, err
	}

	res := &merkleState{
		cfg:          cfg,
		ctx:          ctx,
		metrics:      metrics,
		rewards:      rewards,
		bootstrapped: bootstrapped,
		baseDB:       baseDB,
		baseMerkleDB: baseMerkleDB,
		merkleDB:     merkleDB,
		singletonDB:  singletonDB,

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		delegateeRewardCache:    make(map[ids.NodeID]map[ids.ID]uint64),
		modifiedDelegateeReward: make(map[ids.NodeID]set.Set[ids.ID]),

		modifiedUTXOs:    make(map[ids.ID]*avax.UTXO),
		utxoCache:        &cache.LRU[ids.ID, *avax.UTXO]{Size: utxoCacheSize},
		addedRewardUTXOs: make(map[ids.ID][]*avax.UTXO),
		rewardUTXOsCache: rewardUTXOsCache,

		supplies:      make(map[ids.ID]uint64),
		suppliesCache: suppliesCache,

		addedPermissionedSubnets: make([]*txs.Tx, 0),
		permissionedSubnetCache:  nil, // created first time GetSubnets is called
		addedElasticSubnets:      make(map[ids.ID]*txs.Tx),
		elasticSubnetCache:       transformedSubnetCache,

		addedChains: make(map[ids.ID][]*txs.Tx),
		chainCache:  chainCache,

		addedTxs: make(map[ids.ID]*txAndStatus),
		txCache:  txCache,
		txDB:     txDB,

		addedBlocks: make(map[ids.ID]blocks.Block),
		blockCache:  blockCache,
		blockDB:     blockDB,

		indexedUTXOsDB: indexedUTXOsDB,

		localUptimesCache:    make(map[ids.NodeID]map[ids.ID]*uptimes),
		modifiedLocalUptimes: make(map[ids.NodeID]set.Set[ids.ID]),
		localUptimesDB:       localUptimesDB,

		validatorWeightDiffsCache: validatorWeightDiffsCache,
		localWeightDiffDB:         localWeightDiffDB,

		validatorBlsKeyDiffsCache: validatorBlsKeyDiffsCache,
		localBlsKeyDiffDB:         localBlsKeyDiffDB,
	}

	if err := res.sync(genesisBytes); err != nil {
		// Drop any errors on close to return the first error
		_ = res.Close()
		return nil, err
	}

	return res, nil
}

type merkleState struct {
	cfg          *config.Config
	ctx          *snow.Context
	metrics      metrics.Metrics
	rewards      reward.Calculator
	bootstrapped *utils.Atomic[bool]

	baseDB       *versiondb.Database
	singletonDB  database.Database
	baseMerkleDB database.Database
	merkleDB     merkledb.MerkleDB // meklelized state

	// stakers section (missing Delegatee piece)
	// TODO: Consider moving delegatee to UTXOs section
	currentStakers *baseStakers
	pendingStakers *baseStakers

	delegateeRewardCache    map[ids.NodeID]map[ids.ID]uint64
	modifiedDelegateeReward map[ids.NodeID]set.Set[ids.ID]

	// UTXOs section
	modifiedUTXOs map[ids.ID]*avax.UTXO            // map of UTXO ID -> *UTXO
	utxoCache     cache.Cacher[ids.ID, *avax.UTXO] // UTXO ID -> *UTXO. If the *UTXO is nil the UTXO doesn't exist

	addedRewardUTXOs map[ids.ID][]*avax.UTXO            // map of txID -> []*UTXO
	rewardUTXOsCache cache.Cacher[ids.ID, []*avax.UTXO] // txID -> []*UTXO

	// Metadata section
	chainTime          time.Time
	lastAcceptedBlkID  ids.ID
	lastAcceptedHeight uint64                        // TODO: Should this be written to state??
	supplies           map[ids.ID]uint64             // map of subnetID -> current supply
	suppliesCache      cache.Cacher[ids.ID, *uint64] // cache of subnetID -> current supply if the entry is nil, it is not in the database

	// Subnets section
	addedPermissionedSubnets []*txs.Tx                     // added SubnetTxs, waiting to be committed
	permissionedSubnetCache  []*txs.Tx                     // nil if the subnets haven't been loaded
	addedElasticSubnets      map[ids.ID]*txs.Tx            // map of subnetID -> transformSubnetTx
	elasticSubnetCache       cache.Cacher[ids.ID, *txs.Tx] // cache of subnetID -> transformSubnetTx if the entry is nil, it is not in the database

	// Chains section
	addedChains map[ids.ID][]*txs.Tx            // maps subnetID -> the newly added chains to the subnet
	chainCache  cache.Cacher[ids.ID, []*txs.Tx] // cache of subnetID -> the chains after all local modifications []*txs.Tx

	// Blocks section
	addedBlocks map[ids.ID]blocks.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, blocks.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
	blockDB     database.Database

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

	validatorWeightDiffsCache cache.Cacher[heightWithSubnet, map[ids.NodeID]*ValidatorWeightDiff] // heightWithSubnet -> map[ids.NodeID]*ValidatorWeightDiff
	localWeightDiffDB         database.Database

	validatorBlsKeyDiffsCache cache.Cacher[uint64, map[ids.NodeID]*bls.PublicKey] // cache of height -> map[ids.NodeID]*bls.PublicKey
	localBlsKeyDiffDB         database.Database
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
		prefix = merkleUtxoIndexPrefix(addr)
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

func (ms *merkleState) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := ms.addedRewardUTXOs[txID]; exists {
		return utxos, nil
	}
	if utxos, exists := ms.rewardUTXOsCache.Get(txID); exists {
		return utxos, nil
	}

	utxos := make([]*avax.UTXO, 0)

	prefix := merkleRewardUtxosIDPrefix(txID)

	it := ms.merkleDB.NewIteratorWithPrefix(prefix)
	defer it.Release()

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
	supply, ok := ms.supplies[subnetID]
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
	ms.supplies[subnetID] = cs
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

	transformSubnetTxID, err := database.GetID(ms.merkleDB, key)
	switch err {
	case nil:
		transformSubnetTx, _, err := ms.GetTx(transformSubnetTxID)
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
func (ms *merkleState) GetStatelessBlock(blockID ids.ID) (blocks.Block, error) {
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
		blk, err := blocks.Parse(blocks.GenesisCodec, blkBytes)
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

func (ms *merkleState) AddStatelessBlock(block blocks.Block) {
	ms.addedBlocks[block.ID()] = block
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

// VALIDATORS Section
func (ms *merkleState) ValidatorSet(subnetID ids.ID, vdrs validators.Set) error {
	for nodeID, validator := range ms.currentStakers.validators[subnetID] {
		staker := validator.validator
		if err := vdrs.Add(nodeID, staker.PublicKey, staker.TxID, staker.Weight); err != nil {
			return err
		}

		delegatorIterator := NewTreeIterator(validator.delegators)
		for delegatorIterator.Next() {
			staker := delegatorIterator.Value()
			if err := vdrs.AddWeight(nodeID, staker.Weight); err != nil {
				delegatorIterator.Release()
				return err
			}
		}
		delegatorIterator.Release()
	}
	return nil
}

// TODO: very inefficient implementation until ValidatorDiff optimization is merged in
func (ms *merkleState) GetValidatorWeightDiffs(height uint64, subnetID ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	cacheKey := heightWithSubnet{
		Height:   height,
		SubnetID: subnetID,
	}
	if weightDiffs, ok := ms.validatorWeightDiffsCache.Get(cacheKey); ok {
		return weightDiffs, nil
	}

	// here check in the db
	res := make(map[ids.NodeID]*ValidatorWeightDiff)
	iter := ms.localWeightDiffDB.NewIteratorWithPrefix(subnetID[:])
	defer iter.Release()
	for iter.Next() {
		_, nodeID, retrievedHeight, err := splitMerkleWeightDiffKey(iter.Key())
		switch {
		case err != nil:
			return nil, err
		case retrievedHeight != height:
			continue // loop them all, we'll worry about efficiency after correctness
		default:
			val := &ValidatorWeightDiff{}
			if _, err := blocks.GenesisCodec.Unmarshal(iter.Value(), val); err != nil {
				return nil, err
			}

			res[nodeID] = val
		}
	}
	return res, iter.Error()
}

// TODO: very inefficient implementation until ValidatorDiff optimization is merged in
func (ms *merkleState) GetValidatorPublicKeyDiffs(height uint64) (map[ids.NodeID]*bls.PublicKey, error) {
	if weightDiffs, ok := ms.validatorBlsKeyDiffsCache.Get(height); ok {
		return weightDiffs, nil
	}

	// here check in the db
	res := make(map[ids.NodeID]*bls.PublicKey)
	iter := ms.localBlsKeyDiffDB.NewIterator()
	defer iter.Release()
	for iter.Next() {
		nodeID, retrievedHeight := splitMerkleBlsKeyDiffKey(iter.Key())
		if retrievedHeight != height {
			continue // loop them all, we'll worry about efficiency after correctness
		}

		pkBytes := iter.Value()
		val, err := bls.PublicKeyFromBytes(pkBytes)
		if err != nil {
			return nil, err
		}
		res[nodeID] = val
	}
	return res, iter.Error()
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
	errs := wrappers.Errs{}
	errs.Add(
		ms.localBlsKeyDiffDB.Close(),
		ms.localWeightDiffDB.Close(),
		ms.localUptimesDB.Close(),
		ms.indexedUTXOsDB.Close(),
		ms.txDB.Close(),
		ms.blockDB.Close(),
		ms.merkleDB.Close(),
		ms.baseMerkleDB.Close(),
	)
	return errs.Err
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

	errs := wrappers.Errs{}
	errs.Add(
		ms.writeMerkleState(currentData, pendingData),
		ms.writeBlocks(),
		ms.writeTXs(),
		ms.writeLocalUptimes(),
		ms.writeWeightDiffs(height, weightDiffs),
		ms.writeBlsKeyDiffs(height, blsKeyDiffs),
		ms.updateValidatorSet(updateValidators, valSetDiff, weightDiffs),
	)
	return errs.Err
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
	errs := wrappers.Errs{}
	view, err := ms.merkleDB.NewView()
	if err != nil {
		return err
	}

	ctx := context.TODO()
	errs.Add(
		ms.writeMetadata(view, ctx),
		ms.writePermissionedSubnets(view, ctx),
		ms.writeElasticSubnets(view, ctx),
		ms.writeChains(view, ctx),
		ms.writeCurrentStakers(view, ctx, currentData),
		ms.writePendingStakers(view, ctx, pendingData),
		ms.writeDelegateeRewards(view, ctx),
		ms.writeUTXOs(view, ctx),
		ms.writeRewardUTXOs(view, ctx),
	)
	if errs.Err != nil {
		return err
	}

	return view.CommitToDB(ctx)
}

func (ms *merkleState) writeMetadata(view merkledb.TrieView, ctx context.Context) error {
	encodedChainTime, err := ms.chainTime.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to encoding chainTime: %w", err)
	}
	if err := view.Insert(ctx, merkleChainTimeKey, encodedChainTime); err != nil {
		return fmt.Errorf("failed to write chainTime: %w", err)
	}

	if err := view.Insert(ctx, merkleLastAcceptedBlkIDKey, ms.lastAcceptedBlkID[:]); err != nil {
		return fmt.Errorf("failed to write last accepted: %w", err)
	}

	// lastAcceptedBlockHeight not persisted yet in merkleDB state.
	// TODO: Consider if it should be

	for subnetID, supply := range ms.supplies {
		supply := supply
		delete(ms.supplies, subnetID)
		ms.suppliesCache.Put(subnetID, &supply)

		key := merkleSuppliesKey(subnetID)
		if err := view.Insert(ctx, key, database.PackUInt64(supply)); err != nil {
			return fmt.Errorf("failed to write subnet %v supply: %w", subnetID, err)
		}
	}
	return nil
}

func (ms *merkleState) writePermissionedSubnets(view merkledb.TrieView, ctx context.Context) error {
	for _, subnetTx := range ms.addedPermissionedSubnets {
		key := merklePermissionedSubnetKey(subnetTx.ID())
		if err := view.Insert(ctx, key, subnetTx.Bytes()); err != nil {
			return fmt.Errorf("failed to write subnetTx: %w", err)
		}
	}
	ms.addedPermissionedSubnets = make([]*txs.Tx, 0)
	return nil
}

func (ms *merkleState) writeElasticSubnets(view merkledb.TrieView, ctx context.Context) error {
	for _, subnetTx := range ms.addedElasticSubnets {
		key := merkleElasticSubnetKey(subnetTx.ID())
		if err := view.Insert(ctx, key, subnetTx.Bytes()); err != nil {
			return fmt.Errorf("failed to write subnetTx: %w", err)
		}
	}
	ms.addedElasticSubnets = nil
	return nil
}

func (ms *merkleState) writeChains(view merkledb.TrieView, ctx context.Context) error {
	for subnetID, chains := range ms.addedChains {
		for _, chainTx := range chains {
			key := merkleChainKey(subnetID, chainTx.ID())
			if err := view.Insert(ctx, key, chainTx.Bytes()); err != nil {
				return fmt.Errorf("failed to write chain: %w", err)
			}
		}
		delete(ms.addedChains, subnetID)
	}
	return nil
}

func (*merkleState) writeCurrentStakers(view merkledb.TrieView, ctx context.Context, currentData map[ids.ID]*stakersData) error {
	for stakerTxID, data := range currentData {
		key := merkleCurrentStakersKey(stakerTxID)

		if data.TxBytes == nil {
			if err := view.Remove(ctx, key); err != nil {
				return fmt.Errorf("failed to remove current stakers data, stakerTxID %v: %w", stakerTxID, err)
			}
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize current stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		if err := view.Insert(ctx, key, dataBytes); err != nil {
			return fmt.Errorf("failed to write current stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
	}
	return nil
}

func (*merkleState) writePendingStakers(view merkledb.TrieView, ctx context.Context, pendingData map[ids.ID]*stakersData) error {
	for stakerTxID, data := range pendingData {
		key := merklePendingStakersKey(stakerTxID)

		if data.TxBytes == nil {
			if err := view.Remove(ctx, key); err != nil {
				return fmt.Errorf("failed to write pending stakers data, stakerTxID %v: %w", stakerTxID, err)
			}
			continue
		}

		dataBytes, err := txs.GenesisCodec.Marshal(txs.Version, data)
		if err != nil {
			return fmt.Errorf("failed to serialize pending stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
		if err := view.Insert(ctx, key, dataBytes); err != nil {
			return fmt.Errorf("failed to write pending stakers data, stakerTxID %v: %w", stakerTxID, err)
		}
	}
	return nil
}

func (ms *merkleState) writeUTXOs(view merkledb.TrieView, ctx context.Context) error {
	for utxoID, utxo := range ms.modifiedUTXOs {
		delete(ms.modifiedUTXOs, utxoID)
		key := merkleUtxoIDKey(utxoID)
		if utxo == nil { // delete the UTXO
			switch utxo, err := ms.GetUTXO(utxoID); err {
			case nil:
				ms.utxoCache.Put(utxoID, nil)
				if err := view.Remove(ctx, key); err != nil {
					return err
				}
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
		if err := view.Insert(ctx, key, utxoBytes); err != nil {
			return err
		}

		// store the index
		if err := ms.writeUTXOsIndex(utxo, true /*insertUtxo*/); err != nil {
			return err
		}
	}
	return nil
}

func (ms *merkleState) writeRewardUTXOs(view merkledb.TrieView, ctx context.Context) error {
	for txID, utxos := range ms.addedRewardUTXOs {
		delete(ms.addedRewardUTXOs, txID)
		ms.rewardUTXOsCache.Put(txID, utxos)
		for _, utxo := range utxos {
			utxoBytes, err := txs.GenesisCodec.Marshal(txs.Version, utxo)
			if err != nil {
				return fmt.Errorf("failed to serialize reward UTXO: %w", err)
			}

			key := merkleRewardUtxoIDKey(txID, utxo.InputID())
			if err := view.Insert(ctx, key, utxoBytes); err != nil {
				return fmt.Errorf("failed to add reward UTXO: %w", err)
			}
		}
	}
	return nil
}

func (ms *merkleState) writeDelegateeRewards(view merkledb.TrieView, ctx context.Context) error {
	for nodeID, nodeDelegateeRewards := range ms.modifiedDelegateeReward {
		nodeDelegateeRewardsList := nodeDelegateeRewards.List()
		for _, subnetID := range nodeDelegateeRewardsList {
			delegateeReward := ms.delegateeRewardCache[nodeID][subnetID]

			key := merkleDelegateeRewardsKey(nodeID, subnetID)
			if err := view.Insert(ctx, key, database.PackUInt64(delegateeReward)); err != nil {
				return fmt.Errorf("failed to add reward UTXO: %w", err)
			}
		}
		delete(ms.modifiedDelegateeReward, nodeID)
	}
	return nil
}

func (ms *merkleState) writeBlocks() error {
	for blkID, blk := range ms.addedBlocks {
		blkID := blkID

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

func (ms *merkleState) writeTXs() error {
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

		key := merkleWeightDiffKey(weightKey.subnetID, weightKey.nodeID, height)
		weightDiffBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, weightDiff)
		if err != nil {
			return fmt.Errorf("failed to serialize validator weight diff: %w", err)
		}

		if err := ms.localWeightDiffDB.Put(key, weightDiffBytes); err != nil {
			return fmt.Errorf("failed to add weight diffs: %w", err)
		}

		// update the cache
		cacheKey := heightWithSubnet{
			Height:   height,
			SubnetID: weightKey.subnetID,
		}
		cacheValue := map[ids.NodeID]*ValidatorWeightDiff{
			weightKey.nodeID: weightDiff,
		}
		ms.validatorWeightDiffsCache.Put(cacheKey, cacheValue)
	}
	return nil
}

func (ms *merkleState) writeBlsKeyDiffs(height uint64, blsKeyDiffs map[ids.NodeID]*bls.PublicKey) error {
	for nodeID, blsKey := range blsKeyDiffs {
		key := merkleBlsKeytDiffKey(nodeID, height)
		blsKeyBytes := bls.PublicKeyToBytes(blsKey)

		if err := ms.localBlsKeyDiffDB.Put(key, blsKeyBytes); err != nil {
			return fmt.Errorf("failed to add bls key diffs: %w", err)
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

		// We only track the current validator set of tracked subnets.
		if subnetID != constants.PrimaryNetworkID && !ms.cfg.TrackedSubnets.Contains(subnetID) {
			continue
		}
		if weightDiff.Amount == 0 {
			// No weight change to record; go to next validator.
			continue
		}

		if weightDiff.Decrease {
			err = validators.RemoveWeight(ms.cfg.Validators, subnetID, nodeID, weightDiff.Amount)
		} else {
			if validatorDiff.validatorStatus == added {
				staker := validatorDiff.validator
				err = validators.Add(
					ms.cfg.Validators,
					subnetID,
					nodeID,
					staker.PublicKey,
					staker.TxID,
					weightDiff.Amount,
				)
			} else {
				err = validators.AddWeight(ms.cfg.Validators, subnetID, nodeID, weightDiff.Amount)
			}
		}
		if err != nil {
			return fmt.Errorf("failed to update validator weight: %w", err)
		}
	}
	return nil
}
