// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

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

type chainState interface {
	GetState() state.State
}

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
	statelessBlockState
	// verifier
	// acceptor
	// rejector
	// stateless.BlockVerifier
	// stateless.BlockAcceptor
	// stateless.BlockRejector
	// stateless.Statuser
	// stateless.Timestamper
	baseStateSetter
	// conflictChecker
	chainState
	OnAcceptor
	initialPreferenceGetter
	state.LastAccepteder
	GetBlock(id ids.ID) (snowman.Block, error)
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
		verifiedBlocks:      make(map[ids.ID]stateless.Block),
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
		rejector:        &rejector{backend: backend},
		baseStateSetter: &baseStateSetterImpl{backend: backend},
		// Timestamper:             &timestampGetterImpl{backend: backend},
		initialPreferenceGetter: &initialPreferenceGetterImpl{backend: backend},
		// Statuser:                &statusGetterImpl{backend: backend},
	}
	return manager
}

type manager struct {
	backend
	verifier stateless.Visitor
	acceptor stateless.Visitor
	rejector stateless.Visitor
	// stateless.BlockVerifier
	// stateless.BlockAcceptor
	// stateless.BlockRejector
	// stateless.Statuser
	// stateless.Timestamper
	baseStateSetter
	initialPreferenceGetter
}

func (m *manager) GetState() state.State {
	return m.state
}

func (m *manager) GetLastAccepted() ids.ID {
	return m.state.GetLastAccepted()
}

func (m *manager) SetLastAccepted(blkID ids.ID, persist bool) {
	m.state.SetLastAccepted(blkID, persist)
}

// TODO fix
func (m *manager) GetBlock(blkID ids.ID) (snowman.Block, error) {
	// if blk, ok := m.verifiedBlocks[blkID]; ok {
	// 	return blk, nil
	// }
	// blk, _, err := m.statelessBlockState.GetStatelessBlock(blkID)
	// return blk, err
	return nil, errors.New("TODO")
}
