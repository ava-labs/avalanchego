// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
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
	blockState
	// verifier
	// acceptor
	// rejector
	stateless.BlockVerifier
	stateless.BlockAcceptor
	stateless.BlockRejector
	baseStateSetter
	// conflictChecker
	chainState
	timestampGetter
	OnAcceptor
	initialPreferenceGetter
	statusGetter
	state.LastAccepteder
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	s state.State,
	txExecutorBackend executor.Backend,
	recentlyAccepted *window.Window,
) Manager {
	blockState := &blockStateImpl{
		manager:             nil, // Set below
		statelessBlockState: s,
		verifiedBlks:        map[ids.ID]Block{},
		ctx:                 txExecutorBackend.Ctx,
	}

	backend := backend{
		Mempool:      mempool,
		blockState:   blockState,
		heightSetter: s,
		state:        s,
		bootstrapped: txExecutorBackend.Bootstrapped,
		ctx:          txExecutorBackend.Ctx,
		// blkIDToStatus:         make(map[ids.ID]choices.Status),
		// blkIDToOnAcceptFunc:   make(map[ids.ID]func()),
		// blkIDToOnAcceptState:  make(map[ids.ID]state.Diff),
		// blkIDToOnCommitState:  make(map[ids.ID]state.Diff),
		// blkIDToOnAbortState:   make(map[ids.ID]state.Diff),
		// blkIDToChildren:       make(map[ids.ID][]Block),
		// blkIDToTimestamp:      make(map[ids.ID]time.Time),
		// blkIDToInputs:         make(map[ids.ID]ids.Set),
		// blkIDToAtomicRequests: make(map[ids.ID]map[ids.ID]*atomic.Requests),
		// blkIDToPreferCommit:   make(map[ids.ID]bool),
	}

	manager := &manager{
		backend: backend,
		// verifier: &verifierImpl{
		// 	backend:           backend,
		// 	txExecutorBackend: txExecutorBackend,
		// },
		BlockVerifier: &verifier2{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		// acceptor: &acceptorImpl{
		// 	backend:          backend,
		// 	metrics:          metrics,
		// 	recentlyAccepted: recentlyAccepted,
		// },
		BlockAcceptor: &acceptor2{
			backend:          backend,
			metrics:          metrics,
			recentlyAccepted: recentlyAccepted,
		},
		// rejector:                &rejectorImpl{backend: backend},
		BlockRejector:   &rejector2{backend: backend},
		baseStateSetter: &baseStateSetterImpl{backend: backend},
		// conflictChecker:         &conflictCheckerImpl{backend: backend},
		timestampGetter:         &timestampGetterImpl{backend: backend},
		initialPreferenceGetter: &initialPreferenceGetterImpl{backend: backend},
		statusGetter:            &statusGetterImpl{backend: backend},
	}
	// TODO is there a way to avoid having a Manager
	// in [blockState] so we don't have to do this?
	blockState.manager = manager
	return manager
}

type manager struct {
	backend
	// verifier
	// acceptor
	// rejector
	stateless.BlockVerifier
	stateless.BlockAcceptor
	stateless.BlockRejector
	baseStateSetter
	// conflictChecker
	timestampGetter
	initialPreferenceGetter
	statusGetter
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
