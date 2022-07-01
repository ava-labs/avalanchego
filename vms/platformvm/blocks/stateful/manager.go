// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var _ Manager = &manager{}

type chainState interface {
	GetState() state.State
}

type Manager interface {
	blockState
	verifier
	acceptor
	rejector
	conflictChecker
	freer
	chainState
}

func NewManager(
	mempool mempool.Mempool,
	metrics metrics.Metrics,
	state state.State,
	lastAccepteder lastAccepteder,
	heightSetter heightSetter,
	versionDB versionDB,
	txExecutorBackend executor.Backend,
) Manager {
	blockState := &blockStateImpl{
		manager:           nil, // Set below
		verifiedBlks:      map[ids.ID]Block{},
		txExecutorBackend: txExecutorBackend,
	}

	backend := backend{
		Mempool:        mempool,
		Metrics:        metrics,
		versionDB:      versionDB,
		lastAccepteder: lastAccepteder,
		blockState:     blockState,
		heightSetter:   heightSetter,
		state:          state,
	}

	manager := &manager{
		blockState:      blockState,
		state:           state,
		verifier:        &verifierImpl{backend: backend},
		acceptor:        &acceptorImpl{backend: backend},
		rejector:        &rejectorImpl{backend: backend},
		conflictChecker: &conflictCheckerImpl{backend: backend},
		freer:           &freerImpl{backend: backend},
	}
	// TODO is there a way to avoid having a Manager
	// in [blockState] so we don't have to do this?
	blockState.manager = manager
	return manager
}

type manager struct {
	blockState
	verifier
	acceptor
	rejector
	conflictChecker
	freer
	state state.State
}

func (m *manager) GetState() state.State {
	return m.state
}
