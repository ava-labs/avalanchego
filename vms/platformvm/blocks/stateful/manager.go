// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/metrics"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var _ Manager = &manager{}

type chainState interface {
	GetState() state.State
}

type timestampGetter interface {
	GetTimestamp() time.Time
}

type Manager interface {
	blockState
	verifier
	acceptor
	rejector
	baseStateSetter
	conflictChecker
	freer
	chainState
	timestampGetter
	lastAccepteder

	GetConfig() *config.Config
}

func NewManager(
	mempool mempool.Mempool,
	metrics *metrics.Metrics,
	state state.State,
	heightSetter heightSetter,
	versionDB versionDB,
	timestampGetter timestampGetter,
	txExecutorBackend executor.Backend,
	recentlyAccepted *window.Window,
) Manager {
	blockState := &blockStateImpl{
		manager:      nil, // Set below
		State:        state,
		verifiedBlks: map[ids.ID]Block{},
		ctx:          txExecutorBackend.Ctx,
	}

	backend := backend{
		Mempool:        mempool,
		versionDB:      versionDB,
		lastAccepteder: state,
		blockState:     blockState,
		heightSetter:   heightSetter,
		state:          state,
		bootstrapped:   txExecutorBackend.Bootstrapped,
		ctx:            txExecutorBackend.Ctx,
	}

	manager := &manager{
		backend: backend,
		verifier: &verifierImpl{
			backend:           backend,
			txExecutorBackend: txExecutorBackend,
		},
		acceptor: &acceptorImpl{
			backend:          backend,
			metrics:          metrics,
			recentlyAccepted: recentlyAccepted,
		},
		rejector:        &rejectorImpl{backend: backend},
		baseStateSetter: &baseStateSetterImpl{State: state},
		conflictChecker: &conflictCheckerImpl{backend: backend},
		freer:           &freerImpl{backend: backend},
		timestampGetter: timestampGetter,

		Config: txExecutorBackend.Cfg,
	}
	// TODO is there a way to avoid having a Manager
	// in [blockState] so we don't have to do this?
	blockState.manager = manager
	return manager
}

type manager struct {
	backend
	verifier
	acceptor
	rejector
	baseStateSetter
	conflictChecker
	freer
	timestampGetter

	*config.Config
}

func (m *manager) GetState() state.State {
	return m.state
}

func (m *manager) GetConfig() *config.Config {
	return m.Config
}
