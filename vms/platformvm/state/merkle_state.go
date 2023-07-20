// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

const (
	HistoryLength = int(256)    // from HyperSDK
	NodeCacheSize = int(65_536) // from HyperSDK
)

var (
	_ State = (*merkleState)(nil)

	errNotYetImplemented = errors.New("not yet implemented")

	merkleStatePrefix = []byte{0x0}
	merkleBlockPrefix = []byte{0x1}
	merkleTxPrefix    = []byte{0x2}

	// merkle db sections
	metadataSectionPrefix      = []byte("m")
	merkleChainTimeKey         = append(metadataSectionPrefix, []byte("t")...)
	merkleLastAcceptedBlkIDKey = append(metadataSectionPrefix, []byte("b")...)
	merkleSuppliesPrefix       = append(metadataSectionPrefix, []byte("s")...)

	permissionedSubnetSectionPrefix = []byte("s")
	elasticSubnetSectionPrefix      = []byte("e")
	chainsSectionPrefix             = []byte("c")
)

func NewMerkleState(
	rawDB database.Database,
	metricsReg prometheus.Registerer,
) (Chain, error) {
	var (
		baseDB       = versiondb.New(rawDB)
		baseMerkleDB = prefixdb.New(merkleStatePrefix, baseDB)
		blockDB      = prefixdb.New(merkleBlockPrefix, baseDB)
		txDB         = prefixdb.New(merkleTxPrefix, baseDB)
	)

	ctx := context.TODO()
	noOpTracer, err := trace.New(trace.Config{Enabled: false})
	if err != nil {
		return nil, fmt.Errorf("failed creating noOpTraces: %w", err)
	}

	merkleDB, err := merkledb.New(ctx, baseMerkleDB, merkledb.Config{
		HistoryLength: HistoryLength,
		NodeCacheSize: NodeCacheSize,
		Reg:           prometheus.NewRegistry(),
		Tracer:        noOpTracer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed creating merkleDB: %w", err)
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

	res := &merkleState{
		baseDB:       baseDB,
		baseMerkleDB: baseMerkleDB,
		merkleDB:     merkleDB,
		blockDB:      blockDB,
		txDB:         txDB,

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		ordinaryUTXOs: make(map[ids.ID]*avax.UTXO),
		rewardUTXOs:   make(map[ids.ID][]*avax.UTXO),

		supplies:      make(map[ids.ID]uint64),
		suppliesCache: suppliesCache,

		addedPermissionedSubnets: make([]*txs.Tx, 0),
		permissionedSubnetCache:  make([]*txs.Tx, 0),
		addedElasticSubnets:      make(map[ids.ID]*txs.Tx),
		elasticSubnetCache:       transformedSubnetCache,

		addedChains: make(map[ids.ID][]*txs.Tx),
		chainCache:  chainCache,

		addedTxs: make(map[ids.ID]*txAndStatus),
		txCache:  txCache,

		addedBlocks: make(map[ids.ID]blocks.Block),
		blockCache:  blockCache,
	}
	return res, nil
}

type merkleState struct {
	baseDB       *versiondb.Database
	baseMerkleDB database.Database
	merkleDB     merkledb.MerkleDB // meklelized state
	blockDB      database.Database
	txDB         database.Database

	// stakers section (missing Delegatee piece)
	// TODO: Consider moving delegatee to UTXOs section
	currentStakers *baseStakers
	pendingStakers *baseStakers

	// UTXOs section
	ordinaryUTXOs map[ids.ID]*avax.UTXO   // map of UTXO ID -> *UTXO
	rewardUTXOs   map[ids.ID][]*avax.UTXO // map of txID -> []*UTXO

	// Metadata section
	chainTime          time.Time
	lastAcceptedBlkID  ids.ID
	lastAcceptedHeight uint64                        // Should this be written to state??
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

	// Txs section
	// FIND a way to reduce use of these. No use in verification of addedTxs
	// a limited windows to support APIs
	addedTxs map[ids.ID]*txAndStatus            // map of txID -> {*txs.Tx, Status}
	txCache  cache.Cacher[ids.ID, *txAndStatus] // txID -> {*txs.Tx, Status}. If the entry is nil, it isn't in the database

	// Blocks section
	addedBlocks map[ids.ID]blocks.Block            // map of blockID -> Block
	blockCache  cache.Cacher[ids.ID, blocks.Block] // cache of blockID -> Block. If the entry is nil, it is not in the database
}

// STAKERS section
func (ms *merkleState) GetCurrentValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return ms.currentStakers.GetValidator(subnetID, nodeID)
}

func (ms *merkleState) PutCurrentValidator(staker *Staker) {
	ms.currentStakers.PutValidator(staker)
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

func (*merkleState) GetDelegateeReward( /*subnetID*/ ids.ID /*vdrID*/, ids.NodeID) (amount uint64, err error) {
	return 0, errNotYetImplemented
}

func (*merkleState) SetDelegateeReward( /*subnetID*/ ids.ID /*vdrID*/, ids.NodeID /*amount*/, uint64) error {
	return errNotYetImplemented
}

// UTXOs section
func (ms *merkleState) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := ms.ordinaryUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	return nil, fmt.Errorf("utxos not stored: %w", errNotYetImplemented)
}

func (*merkleState) UTXOIDs( /*addr*/ []byte /*start*/, ids.ID /*limit*/, int) ([]ids.ID, error) {
	return nil, fmt.Errorf("utxos iteration not yet implemented: %w", errNotYetImplemented)
}

func (ms *merkleState) AddUTXO(utxo *avax.UTXO) {
	ms.ordinaryUTXOs[utxo.InputID()] = utxo
}

func (ms *merkleState) DeleteUTXO(utxoID ids.ID) {
	ms.ordinaryUTXOs[utxoID] = nil
}

func (ms *merkleState) GetRewardUTXOs(txID ids.ID) ([]*avax.UTXO, error) {
	if utxos, exists := ms.rewardUTXOs[txID]; exists {
		return utxos, nil
	}

	return nil, fmt.Errorf("reward utxos not stored: %w", errNotYetImplemented)
}

func (ms *merkleState) AddRewardUTXO(txID ids.ID, utxo *avax.UTXO) {
	ms.rewardUTXOs[txID] = append(ms.rewardUTXOs[txID], utxo)
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

	return supply, fmt.Errorf("supply not stored: %w", errNotYetImplemented)
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

	subnetIDKey := make([]byte, 0, len(elasticSubnetSectionPrefix)+len(subnetID[:]))
	copy(subnetIDKey, merkleSuppliesPrefix)
	subnetIDKey = append(subnetIDKey, subnetID[:]...)

	transformSubnetTxID, err := database.GetID(ms.merkleDB, subnetIDKey)
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

	prefix := make([]byte, 0, len(chainsSectionPrefix)+len(subnetID[:]))
	copy(prefix, chainsSectionPrefix)
	prefix = append(prefix, subnetID[:]...)

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
func (*merkleState) GetUptime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
) (upDuration time.Duration, lastUpdated time.Time, err error) {
	return 0, time.Time{}, fmt.Errorf("MerkleDB GetUptime: %w", errNotYetImplemented)
}

func (*merkleState) SetUptime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
	/*upDuration*/ time.Duration,
	/*lastUpdated*/ time.Time,
) error {
	return fmt.Errorf("MerkleDB SetUptime: %w", errNotYetImplemented)
}

func (*merkleState) GetStartTime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
) (startTime time.Time, err error) {
	return time.Time{}, fmt.Errorf("MerkleDB GetStartTime: %w", errNotYetImplemented)
}

// VALIDATORS Section
func (*merkleState) ValidatorSet( /*subnetID*/ ids.ID /*vdrs*/, validators.Set) error {
	return fmt.Errorf("MerkleDB ValidatorSet: %w", errNotYetImplemented)
}

func (*merkleState) GetValidatorWeightDiffs( /*height*/ uint64 /*subnetID*/, ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	return nil, fmt.Errorf("MerkleDB GetValidatorWeightDiffs: %w", errNotYetImplemented)
}

func (*merkleState) GetValidatorPublicKeyDiffs( /*height*/ uint64) (map[ids.NodeID]*bls.PublicKey, error) {
	return nil, fmt.Errorf("MerkleDB GetValidatorPublicKeyDiffs: %w", errNotYetImplemented)
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
	if err := ms.write(true /*=updateValidators*/, ms.lastAcceptedHeight); err != nil {
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
		ms.blockDB.Close(),
		ms.merkleDB.Close(),
		ms.baseMerkleDB.Close(),
	)
	return errs.Err
}

func (ms *merkleState) write( /*updateValidators*/ bool /*height*/, uint64) error {
	errs := wrappers.Errs{}
	errs.Add(
		ms.writeMerkleState(),
		ms.writeBlocks(),
		ms.writeTXs(),
	)
	return errs.Err
}

func (ms *merkleState) writeMerkleState() error {
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

		key := make([]byte, 0, len(merkleSuppliesPrefix)+len(subnetID[:]))
		copy(key, merkleSuppliesPrefix)
		key = append(key, subnetID[:]...)
		if err := view.Insert(ctx, key, database.PackUInt64(supply)); err != nil {
			return fmt.Errorf("failed to write subnet %v supply: %w", subnetID, err)
		}
	}
	return nil
}

func (ms *merkleState) writePermissionedSubnets(view merkledb.TrieView, ctx context.Context) error {
	for _, subnetTx := range ms.addedPermissionedSubnets {
		subnetID := subnetTx.ID()

		key := make([]byte, 0, len(permissionedSubnetSectionPrefix)+len(subnetID[:]))
		copy(key, permissionedSubnetSectionPrefix)
		key = append(key, subnetID[:]...)

		if err := view.Insert(ctx, key, subnetTx.Bytes()); err != nil {
			return fmt.Errorf("failed to write subnetTx: %w", err)
		}
	}
	ms.addedPermissionedSubnets = nil
	return nil
}

func (ms *merkleState) writeElasticSubnets(view merkledb.TrieView, ctx context.Context) error {
	for _, subnetTx := range ms.addedElasticSubnets {
		subnetID := subnetTx.ID()

		key := make([]byte, 0, len(elasticSubnetSectionPrefix)+len(subnetID[:]))
		copy(key, elasticSubnetSectionPrefix)
		key = append(key, subnetID[:]...)

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
			chainID := chainTx.ID()

			key := make([]byte, 0, len(chainsSectionPrefix)+len(subnetID[:]))
			copy(key, chainsSectionPrefix)
			key = append(key, subnetID[:]...)
			key = append(key, chainID[:]...)

			if err := view.Insert(ctx, key, chainTx.Bytes()); err != nil {
				return fmt.Errorf("failed to write chain: %w", err)
			}
		}
		delete(ms.addedChains, subnetID)
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
