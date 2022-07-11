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

type OnAcceptor interface {
	// This function should only be called after Verify is called on [blkID].
	// OnAccept returns:
	// 1) The current state of the chain, if this block is decided or hasn't
	//    been verified.
	// 2) The state of the chain after this block is accepted, if this block was
	//    verified successfully.
	OnAccept(blkID ids.ID) state.Chain
}

type Manager interface {
	OnAcceptor
	GetBlock(id ids.ID) (snowman.Block, error)
	NewBlock(stateless.Block) snowman.Block
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend executor.Backend,
	recentlyAccepted *window.Window,
) Manager {
	backend := backend{
		Mempool:             mempool,
		statelessBlockState: s,
		heightSetter:        s,
		state:               s,
		bootstrapped:        txExecutorBackend.Bootstrapped,
		ctx:                 txExecutorBackend.Ctx,
		blkIDToState:        map[ids.ID]*blockState{},
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
	backend
	verifier stateless.Visitor
	acceptor stateless.Visitor
	rejector stateless.Visitor
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	if blk, ok := m.blkIDToState[blkID]; ok {
		return newBlock(blk.statelessBlock, m), nil
	}
	statelessBlk, _, err := m.backend.state.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return newBlock(statelessBlk, m), nil
}

func (m *manager) NewBlock(blk stateless.Block) snowman.Block {
	return newBlock(blk, m)
}
