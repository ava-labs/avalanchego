// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators"
)

var _ Manager = (*manager)(nil)

type Manager interface {
	state.Versions

	// Returns the ID of the most recently accepted block.
	LastAccepted() ids.ID
	GetBlock(blkID ids.ID) (snowman.Block, error)
	GetStatelessBlock(blkID ids.ID) (blocks.Block, error)
	NewBlock(blocks.Block) snowman.Block
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	acceptLock *sync.RWMutex,
	s state.State,
	txExecutorBackend *executor.Backend,
	validatorManager validators.Manager,
) Manager {
	backend := &backend{
		Mempool:      mempool,
		lastAccepted: s.GetLastAccepted(),
		acceptLock:   acceptLock,
		state:        s,
		ctx:          txExecutorBackend.Ctx,
		blkIDToState: map[ids.ID]*blockState{},
	}

	return &manager{
		backend: backend,
		verifier: &verifier{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		acceptor: &acceptor{
			backend:      backend,
			metrics:      metrics,
			validators:   validatorManager,
			bootstrapped: txExecutorBackend.Bootstrapped,
		},
		rejector: &rejector{
			backend:         backend,
			addTxsToMempool: !txExecutorBackend.Config.PartialSyncPrimaryNetwork,
		},
	}
}

type manager struct {
	*backend
	verifier blocks.Visitor
	acceptor blocks.Visitor
	rejector blocks.Visitor
}

func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	blk, err := m.backend.GetBlock(blkID)
	if err != nil {
		return nil, err
	}
	return m.NewBlock(blk), nil
}

func (m *manager) GetStatelessBlock(blkID ids.ID) (blocks.Block, error) {
	return m.backend.GetBlock(blkID)
}

func (m *manager) NewBlock(blk blocks.Block) snowman.Block {
	return &Block{
		manager: m,
		Block:   blk,
	}
}
