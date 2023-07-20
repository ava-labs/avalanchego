// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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

	ErrNotYetImplemented = errors.New("not yet implemented")

	merkleStatePrefix = []byte{0x0}
	merkleBlockPrefix = []byte{0x1}
)

func NewMerkleState(rawDB database.Database) (Chain, error) {
	var (
		baseDB       = versiondb.New(rawDB)
		baseMerkleDB = prefixdb.New(merkleStatePrefix, baseDB)
		blockDB      = prefixdb.New(merkleBlockPrefix, baseDB)
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

	res := &merkleState{
		baseDB:       baseDB,
		baseMerkleDB: baseMerkleDB,
		merkleDB:     merkleDB,
		blockDB:      blockDB,

		currentStakers: newBaseStakers(),
		pendingStakers: newBaseStakers(),

		ordinaryUTXOs: make(map[ids.ID]*avax.UTXO),
		rewardUTXOs:   make(map[ids.ID][]*avax.UTXO),

		supplies: make(map[ids.ID]uint64),

		subnets:            make([]*txs.Tx, 0),
		transformedSubnets: make(map[ids.ID]*txs.Tx),

		chains: make(map[ids.ID][]*txs.Tx),

		txs: make(map[ids.ID]*txAndStatus),
	}
	return res, nil
}

type merkleState struct {
	baseDB       database.Database
	baseMerkleDB database.Database
	merkleDB     merkledb.MerkleDB // meklelized state
	blockDB      database.Database // all the rest, prolly just blocks??

	// TODO ABENEGIA: in sections below there should ofter be three elements
	// in-memory element to track non-committed diffs
	// a cache of the DB
	// the DB
	// For now the in-memory diff and cache are treated the same. I'll introduce
	// as soon as there will be persistence/commit

	// stakers section (missing Delegatee piece)
	// TODO: Consider moving delegatee to UTXOs section
	currentStakers *baseStakers
	pendingStakers *baseStakers

	// UTXOs section
	ordinaryUTXOs map[ids.ID]*avax.UTXO   // map of UTXO ID -> *UTXO
	rewardUTXOs   map[ids.ID][]*avax.UTXO // map of txID -> []*UTXO

	// Metadata section
	chainTime          time.Time
	supplies           map[ids.ID]uint64 // map of subnetID -> current supply
	lastAcceptedBlkID  ids.ID
	lastAcceptedHeight uint64

	// Subnets section
	subnets            []*txs.Tx
	transformedSubnets map[ids.ID]*txs.Tx // map of subnetID -> transformSubnetTx

	// Chains section
	chains map[ids.ID][]*txs.Tx // maps subnetID -> subnet's chains

	// Txs section
	// FIND a way to reduce use of these. No use in verification of txs
	// a limited windows to support APIs
	txs map[ids.ID]*txAndStatus

	// Blocks section
	blocks map[ids.ID]blocks.Block // map of blockID -> Block
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
	return 0, ErrNotYetImplemented
}

func (*merkleState) SetDelegateeReward( /*subnetID*/ ids.ID /*vdrID*/, ids.NodeID /*amount*/, uint64) error {
	return ErrNotYetImplemented
}

// UTXOs section
func (ms *merkleState) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, exists := ms.ordinaryUTXOs[utxoID]; exists {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}
	return nil, fmt.Errorf("utxos not stored: %w", ErrNotYetImplemented)
}

func (*merkleState) UTXOIDs( /*addr*/ []byte /*start*/, ids.ID /*limit*/, int) ([]ids.ID, error) {
	return nil, fmt.Errorf("utxos iteration not yet implemented: %w", ErrNotYetImplemented)
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

	return nil, fmt.Errorf("reward utxos not stored: %w", ErrNotYetImplemented)
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

	return supply, fmt.Errorf("supply not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) SetCurrentSupply(subnetID ids.ID, cs uint64) {
	ms.supplies[subnetID] = cs
}

// SUBNETS Section
func (ms *merkleState) GetSubnets() ([]*txs.Tx, error) {
	if ms.subnets != nil {
		return ms.subnets, nil
	}

	return nil, fmt.Errorf("subnets not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) AddSubnet(createSubnetTx *txs.Tx) {
	ms.subnets = append(ms.subnets, createSubnetTx)
}

func (ms *merkleState) GetSubnetTransformation(subnetID ids.ID) (*txs.Tx, error) {
	if tx, exists := ms.transformedSubnets[subnetID]; exists {
		return tx, nil
	}

	return nil, fmt.Errorf("transformed subnets not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) AddSubnetTransformation(transformSubnetTxIntf *txs.Tx) {
	transformSubnetTx := transformSubnetTxIntf.Unsigned.(*txs.TransformSubnetTx)
	ms.transformedSubnets[transformSubnetTx.Subnet] = transformSubnetTxIntf
}

// CHAINS Section
func (ms *merkleState) GetChains(subnetID ids.ID) ([]*txs.Tx, error) {
	if chains, cached := ms.chains[subnetID]; cached {
		return chains, nil
	}

	return nil, fmt.Errorf("chains not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) AddChain(createChainTxIntf *txs.Tx) {
	createChainTx := createChainTxIntf.Unsigned.(*txs.CreateChainTx)
	subnetID := createChainTx.SubnetID

	ms.chains[subnetID] = append(ms.chains[subnetID], createChainTxIntf)
}

// TXs Section
func (ms *merkleState) GetTx(txID ids.ID) (*txs.Tx, status.Status, error) {
	if tx, exists := ms.txs[txID]; exists {
		return tx.tx, tx.status, nil
	}

	return nil, status.Unknown, fmt.Errorf("txs not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) AddTx(tx *txs.Tx, status status.Status) {
	ms.txs[tx.ID()] = &txAndStatus{
		tx:     tx,
		status: status,
	}
}

// BLOCKs Section
func (ms *merkleState) GetStatelessBlock(blockID ids.ID) (blocks.Block, error) {
	if blk, exists := ms.blocks[blockID]; exists {
		return blk, nil
	}

	return nil, fmt.Errorf("blocks not stored: %w", ErrNotYetImplemented)
}

func (ms *merkleState) AddStatelessBlock(block blocks.Block) {
	ms.blocks[block.ID()] = block
}

// UPTIMES SECTION
func (*merkleState) GetUptime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
) (upDuration time.Duration, lastUpdated time.Time, err error) {
	return 0, time.Time{}, fmt.Errorf("MerkleDB GetUptime: %w", ErrNotYetImplemented)
}

func (*merkleState) SetUptime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
	/*upDuration*/ time.Duration,
	/*lastUpdated*/ time.Time,
) error {
	return fmt.Errorf("MerkleDB SetUptime: %w", ErrNotYetImplemented)
}

func (*merkleState) GetStartTime(
	/*nodeID*/ ids.NodeID,
	/*subnetID*/ ids.ID,
) (startTime time.Time, err error) {
	return time.Time{}, fmt.Errorf("MerkleDB GetStartTime: %w", ErrNotYetImplemented)
}

// VALIDATORS Section
func (*merkleState) ValidatorSet( /*subnetID*/ ids.ID /*vdrs*/, validators.Set) error {
	return fmt.Errorf("MerkleDB ValidatorSet: %w", ErrNotYetImplemented)
}

func (*merkleState) GetValidatorWeightDiffs( /*height*/ uint64 /*subnetID*/, ids.ID) (map[ids.NodeID]*ValidatorWeightDiff, error) {
	return nil, fmt.Errorf("MerkleDB GetValidatorWeightDiffs: %w", ErrNotYetImplemented)
}

func (*merkleState) GetValidatorPublicKeyDiffs( /*height*/ uint64) (map[ids.NodeID]*bls.PublicKey, error) {
	return nil, fmt.Errorf("MerkleDB GetValidatorPublicKeyDiffs: %w", ErrNotYetImplemented)
}

// DB Operations
func (*merkleState) Abort() {}

func (*merkleState) Commit() error {
	return fmt.Errorf("MerkleDB Commit: %w", ErrNotYetImplemented)
}

func (*merkleState) CommitBatch() (database.Batch, error) {
	return nil, fmt.Errorf("MerkleDB CommitBatch: %w", ErrNotYetImplemented)
}

func (*merkleState) Checksum() ids.ID {
	return ids.Empty
}

func (*merkleState) Close() error {
	return fmt.Errorf("MerkleDB Close: %w", ErrNotYetImplemented)
}
