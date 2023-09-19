// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/avm/states"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
)

var (
	_ Manager = (*manager)(nil)

	ErrChainNotSynced       = errors.New("chain not synced")
	ErrConflictingParentTxs = errors.New("block contains a transaction that conflicts with a transaction in a parent block")
)

type Manager interface {
	states.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID

	SetPreference(blkID ids.ID)
	Preferred() ids.ID

	GetBlock(blkID ids.ID) (snowman.Block, error)
	GetStatelessBlock(blkID ids.ID) (block.Block, error)
	NewBlock(block.Block) snowman.Block

	// VerifyTx verifies that the transaction can be issued based on the
	// currently preferred state.
	VerifyTx(tx *txs.Tx) error

	// VerifyUniqueInputs verifies that the inputs are not duplicated in the
	// provided blk or any of its ancestors pinned in memory.
	VerifyUniqueInputs(blkID ids.ID, inputs set.Set[ids.ID]) error
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	state states.State,
	backend *executor.Backend,
	clk *mockable.Clock,
	onAccept func(*txs.Tx) error,
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
	state   states.State
	metrics metrics.Metrics
	mempool mempool.Mempool
	clk     *mockable.Clock
	// Invariant: onAccept is called when [tx] is being marked as accepted, but
	// before its state changes are applied.
	// Invariant: any error returned by onAccept should be considered fatal.
	onAccept func(*txs.Tx) error

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
	onAcceptState  states.Diff
	importedInputs set.Set[ids.ID]
	atomicRequests map[ids.ID]*atomic.Requests
}

func (m *manager) GetState(blkID ids.ID) (states.Chain, bool) {
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

	stateDiff, err := states.NewDiff(m.preferred, m)
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
	err = tx.Unsigned.Visit(executor)
	if err != nil {
		return err
	}

	return m.VerifyUniqueInputs(m.preferred, executor.Inputs)
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
