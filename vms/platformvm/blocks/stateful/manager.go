// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var _ Manager = &manager{}

type Manager interface {
	// This function should only be called after Verify is called on [blkID].
	// OnAccept returns:
	// 1) The current state of the chain, if this block is decided or hasn't
	//    been verified.
	// 2) The state of the chain after this block is accepted, if this block was
	//    verified successfully.
	OnAccept(blkID ids.ID) state.Chain
	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID
	GetBlock(id ids.ID) (snowman.Block, error)
	NewBlock(stateless.Block) snowman.Block

	ExpectedChildVersion(blk snowman.Block) uint16
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend executor.Backend,
	recentlyAccepted *window.Window,
) Manager {
	backend := &backend{
		Mempool:      mempool,
		state:        s,
		bootstrapped: txExecutorBackend.Bootstrapped,
		ctx:          txExecutorBackend.Ctx,
		cfg:          txExecutorBackend.Config,
		blkIDToState: map[ids.ID]*blockState{},
	}

	verifier := &verifier{
		backend:           backend,
		txExecutorBackend: txExecutorBackend,
	}
	manager := &manager{
		backend:  backend,
		verifier: verifier,
		acceptor: &acceptor{
			backend:          backend,
			metrics:          metrics,
			recentlyAccepted: recentlyAccepted,
		},
		rejector: &rejector{backend: backend},
	}

	// TODO ABENEGIA: solve this loop
	verifier.man = manager
	return manager
}

type manager struct {
	*backend
	verifier stateless.Visitor
	acceptor stateless.Visitor
	rejector stateless.Visitor
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	// See if the block is in memory.
	if blk, ok := m.blkIDToState[blkID]; ok {
		return newBlock(blk.statelessBlock, m), nil
	}
	// The block isn't in memory. Check the database.
	statelessBlk, _, err := m.backend.state.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return newBlock(statelessBlk, m), nil
}

func (m *manager) NewBlock(blk stateless.Block) snowman.Block {
	return newBlock(blk, m)
}

func (m *manager) LastAccepted() ids.ID {
	if m.backend.lastAccepted == ids.Empty {
		// No blocks have been accepted since startup.
		// Return the last accepted block from state.
		return m.state.GetLastAccepted()
	}
	return m.backend.lastAccepted
}

func newBlock(blk stateless.Block, manager *manager) snowman.Block {
	b := &Block{
		manager: manager,
		Block:   blk,
	}
	// TODO should we just have a NewOracleBlock method?
	switch blk.(type) {
	case *stateless.BlueberryProposalBlock,
		*stateless.ApricotProposalBlock:
		return &OracleBlock{
			Block: b,
		}
	default:
		return b
	}
}
