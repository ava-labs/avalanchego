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
	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID
	GetBlock(id ids.ID) (snowman.Block, error)
	NewBlock(stateless.Block) snowman.Block
}

func NewManager(
	mempool mempool.Mempool,
	metrics *metrics.Metrics,
	s state.State,
	txExecutorBackend executor.Backend,
	recentlyAccepted *window.Window,
) Manager {
	backend := &backend{
		Mempool:       mempool,
		state:         s,
		bootstrapped:  txExecutorBackend.Bootstrapped,
		ctx:           txExecutorBackend.Ctx,
		blkIDToState:  map[ids.ID]*blockState{},
		stateVersions: txExecutorBackend.StateVersions,
	}

	manager := &manager{
		backend: backend,
		verifier: &verifier{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		acceptor: &acceptor{
			backend:          backend,
			metrics:          metrics,
			recentlyAccepted: recentlyAccepted,
		},
		rejector: &rejector{backend: backend},
	}
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
	if _, ok := blk.(*stateless.ProposalBlock); ok {
		return &OracleBlock{
			Block: b,
		}
	}
	return b
}
