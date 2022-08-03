// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
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
	NewBlock(blocks.Block) snowman.Block

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
		Mempool:       mempool,
		state:         s,
		bootstrapped:  txExecutorBackend.Bootstrapped,
		ctx:           txExecutorBackend.Ctx,
		cfg:           txExecutorBackend.Config,
		blkIDToState:  map[ids.ID]*blockState{},
		stateVersions: txExecutorBackend.StateVersions,
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
	verifier blocks.Visitor
	acceptor blocks.Visitor
	rejector blocks.Visitor
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	statelessBlk, err := m.getStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(statelessBlk), nil
}

func (m *manager) NewBlock(blk blocks.Block) snowman.Block {
	return &Block{
		manager: m,
		Block:   blk,
	}
}

func (m *manager) LastAccepted() ids.ID {
	if m.backend.lastAccepted == ids.Empty {
		// No blocks have been accepted since startup.
		// Return the last accepted block from state.
		return m.state.GetLastAccepted()
	}
	return m.backend.lastAccepted
}
