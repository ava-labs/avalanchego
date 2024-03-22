// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	_ Diff     = (*diff)(nil)
	_ Versions = stateGetter{}

	ErrMissingParentState = errors.New("missing parent state")
)

type Diff interface {
	Chain

	Apply(Chain)
}

type diff struct {
	parentID      ids.ID
	stateVersions Versions

	// map of modified UTXOID -> *UTXO if the UTXO is nil, it has been removed
	modifiedUTXOs map[ids.ID]*avax.UTXO
	addedTxs      map[ids.ID]*txs.Tx     // map of txID -> tx
	addedBlockIDs map[uint64]ids.ID      // map of height -> blockID
	addedBlocks   map[ids.ID]block.Block // map of blockID -> block

	lastAccepted      ids.ID
	timestamp         time.Time
	unitFees          *commonfees.Dimensions
	lastBlkComplexity *commonfees.Dimensions
}

func NewDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}
	return &diff{
		parentID:      parentID,
		stateVersions: stateVersions,
		modifiedUTXOs: make(map[ids.ID]*avax.UTXO),
		addedTxs:      make(map[ids.ID]*txs.Tx),
		addedBlockIDs: make(map[uint64]ids.ID),
		addedBlocks:   make(map[ids.ID]block.Block),
		lastAccepted:  parentState.GetLastAccepted(),
		timestamp:     parentState.GetTimestamp(),
	}, nil
}

type stateGetter struct {
	state Chain
}

func (s stateGetter) GetState(ids.ID) (Chain, bool) {
	return s.state, true
}

func NewDiffOn(parentState Chain) (Diff, error) {
	return NewDiff(ids.Empty, stateGetter{
		state: parentState,
	})
}

func (d *diff) GetUTXO(utxoID ids.ID) (*avax.UTXO, error) {
	if utxo, modified := d.modifiedUTXOs[utxoID]; modified {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetUTXO(utxoID)
}

func (d *diff) AddUTXO(utxo *avax.UTXO) {
	d.modifiedUTXOs[utxo.InputID()] = utxo
}

func (d *diff) DeleteUTXO(utxoID ids.ID) {
	d.modifiedUTXOs[utxoID] = nil
}

func (d *diff) GetTx(txID ids.ID) (*txs.Tx, error) {
	if tx, exists := d.addedTxs[txID]; exists {
		return tx, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetTx(txID)
}

func (d *diff) AddTx(tx *txs.Tx) {
	d.addedTxs[tx.ID()] = tx
}

func (d *diff) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	if blkID, exists := d.addedBlockIDs[height]; exists {
		return blkID, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return ids.Empty, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetBlockIDAtHeight(height)
}

func (d *diff) GetBlock(blkID ids.ID) (block.Block, error) {
	if blk, exists := d.addedBlocks[blkID]; exists {
		return blk, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetBlock(blkID)
}

func (d *diff) AddBlock(blk block.Block) {
	blkID := blk.ID()
	d.addedBlockIDs[blk.Height()] = blkID
	d.addedBlocks[blkID] = blk
}

func (d *diff) GetLastAccepted() ids.ID {
	return d.lastAccepted
}

func (d *diff) SetLastAccepted(lastAccepted ids.ID) {
	d.lastAccepted = lastAccepted
}

func (d *diff) GetTimestamp() time.Time {
	return d.timestamp
}

func (d *diff) SetTimestamp(t time.Time) {
	d.timestamp = t
}

func (d *diff) GetFeeRates() (commonfees.Dimensions, error) {
	if d.unitFees == nil {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return commonfees.Empty, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		parentUnitFees, err := parentState.GetFeeRates()
		if err != nil {
			return commonfees.Empty, err
		}

		d.unitFees = new(commonfees.Dimensions)
		*d.unitFees = parentUnitFees
	}

	return *d.unitFees, nil
}

func (d *diff) SetFeeRates(uf commonfees.Dimensions) {
	if d.unitFees == nil {
		d.unitFees = new(commonfees.Dimensions)
	}
	*d.unitFees = uf
}

func (d *diff) GetLastBlockComplexity() (commonfees.Dimensions, error) {
	if d.lastBlkComplexity == nil {
		parentState, ok := d.stateVersions.GetState(d.parentID)
		if !ok {
			return commonfees.Empty, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
		}
		parentFeeWindows, err := parentState.GetLastBlockComplexity()
		if err != nil {
			return commonfees.Empty, err
		}

		d.lastBlkComplexity = new(commonfees.Dimensions)
		*d.lastBlkComplexity = parentFeeWindows
	}

	return *d.lastBlkComplexity, nil
}

func (d *diff) SetLastBlockComplexity(windows commonfees.Dimensions) {
	if d.lastBlkComplexity == nil {
		d.lastBlkComplexity = new(commonfees.Dimensions)
	}
	*d.lastBlkComplexity = windows
}

func (d *diff) Apply(state Chain) {
	for utxoID, utxo := range d.modifiedUTXOs {
		if utxo != nil {
			state.AddUTXO(utxo)
		} else {
			state.DeleteUTXO(utxoID)
		}
	}

	for _, tx := range d.addedTxs {
		state.AddTx(tx)
	}

	for _, blk := range d.addedBlocks {
		state.AddBlock(blk)
	}

	state.SetLastAccepted(d.lastAccepted)
	state.SetTimestamp(d.timestamp)
	if d.unitFees != nil {
		state.SetFeeRates(*d.unitFees)
	}
	if d.lastBlkComplexity != nil {
		state.SetLastBlockComplexity(*d.lastBlkComplexity)
	}
}
