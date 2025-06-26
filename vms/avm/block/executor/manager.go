// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/avm/block"
	"github.com/ava-labs/avalanchego/vms/avm/metrics"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced       = errors.New("chain not synced")
	ErrConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (snowman.Block, error)
	GetStatelessBlock(blkID ids.ID) (block.Block, error)
	NewBlock(block.Block) snowman.Block

	// VerifyTx verifies that the transaction can be issued based on the currently
	// preferred state. This should *not* be used to verify transactions in a block.
	VerifyTx(tx *txs.Tx) error

	// VerifyUniqueInputs returns nil iff no blocks in the inclusive
	// ancestry of [blkID] consume an input in [inputs].
	VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error
}

func NewManager(
	mempool mempool.Mempool[*txs.Tx],
	metrics metrics.Metrics,
	state state.State,
	backend *executor.Backend,
	clk *mockable.Clock,
	onAccept func(*txs.Tx),
) Manager {
	lastAccepted := state.GetLastAccepted()
	return &manager{
		backend:      backend,
		state:        state,
		metrics:      metrics,
		mempool:      mempool,
		clk:          clk,
		onAccept:     onAccept,
		blkIDToState: map[ids.ID]*blockState{},
		lastAccepted: lastAccepted,
		preferred:    lastAccepted,
	}
}

type manager struct {
	backend *executor.Backend
	state   state.State
	metrics metrics.Metrics
	mempool mempool.Mempool[*txs.Tx]
	clk     *mockable.Clock
	// Invariant: onAccept is called when [tx] is being marked as accepted, but
	// before its state changes are applied.
	onAccept func(*txs.Tx)

	// blkIDToState is a map from a block's ID to the state of the block.
	// Blocks are put into this map when they are verified.
	// Blocks are removed from this map when they are decided.
	blkIDToState map[ids.ID]*blockState

	// lastAccepted is the ID of the last block that had Accept() called on it.
	lastAccepted ids.ID
	preferred    ids.ID
}

type blockState struct {
	statelessBlock block.Block
	onAcceptState  state.Diff
	importedInputs set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests
}

func (m *manager) GetState(blkID ids.ID) (state.Chain, bool) {
	// If the block is in the map, it is processing.
	if state, ok := m.blkIDToState[blkID]; ok {
		return state.onAcceptState, true
	}
	return m.state, blkID == m.lastAccepted
}

func (m *manager) LastAccepted() ids.ID {
	return m.lastAccepted
}

func (m *manager) SetPreference(blockID ids.ID) {
	m.preferred = blockID
}

func (m *manager) Preferred() ids.ID {
	return m.preferred
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	blk, err := m.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (block.Block, error) {
	// See if the block is in memory.
	if blkState, ok := m.blkIDToState[blkID]; ok {
		return blkState.statelessBlock, nil
	}
	// The block isn't in memory. Check the database.
	return m.state.GetBlock(blkID)
}

func (m *manager) NewBlock(blk block.Block) snowman.Block {
	return &Block{
		Block:   blk,
		manager: m,
	}
}

func (m *manager) VerifyTx(tx *txs.Tx) error {
	if !m.backend.Bootstrapped {
		return ErrChainNotSynced
	}

	err := tx.Unsigned.Visit(&executor.SyntacticVerifier{
		Backend: m.backend,
		Tx:      tx,
	})
	if err != nil {
		return err
	}

	stateDiff, err := state.NewDiff(m.lastAccepted, m)
	if err != nil {
		return err
	}

	err = tx.Unsigned.Visit(&executor.SemanticVerifier{
		Backend: m.backend,
		State:   stateDiff,
		Tx:      tx,
	})
	if err != nil {
		return err
	}

	executor := &executor.Executor{
		Codec: m.backend.Codec,
		State: stateDiff,
		Tx:    tx,
	}
	return tx.Unsigned.Visit(executor)
}

func (m *manager) VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error {
	if inputs.Len() == 0 {
		return nil
	}

	// Check for conflicts in ancestors.
	for {
		state, ok := m.blkIDToState[blkID]
		if !ok {
			// The parent state isn't pinned in memory.
			// This means the parent must be accepted already.
			return nil
		}

		if state.importedInputs.Overlaps(inputs) {
			return ErrConflictingParentTxs
		}

		blk := state.statelessBlock
		blkID = blk.Parent()
	}
}

func (m *manager) free(blkID ids.ID) {
	delete(m.blkIDToState, blkID)
}
