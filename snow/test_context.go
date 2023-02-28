// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"testing"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

func DefaultContextTest() *Context {
	return &Context{
		NetworkID:    0,
		SubnetID:     ids.Empty,
		ChainID:      ids.Empty,
		NodeID:       ids.EmptyNodeID,
		Log:          logging.NoLog{},
		BCLookup:     ids.NewAliaser(),
		Metrics:      metrics.NewOptionalGatherer(),
		ChainDataDir: "",
	}
}

func DefaultConsensusContextTest(t *testing.T) *ConsensusContext {
	var (
		startedState State = Initializing
		stoppedState State = Initializing
	)

	return &ConsensusContext{
		Context:             DefaultContextTest(),
		Registerer:          prometheus.NewRegistry(),
		AvalancheRegisterer: prometheus.NewRegistry(),
		DecisionAcceptor:    noOpAcceptor{},
		ConsensusAcceptor:   noOpAcceptor{},
		SubnetStateTracker: &SubnetStateTrackerTest{
			T: t,
			IsSubnetSyncedF: func() bool {
				return stoppedState == Bootstrapping || stoppedState == StateSyncing
			},
			IsChainBootstrappedF: func(ids.ID) bool {
				return stoppedState == Bootstrapping || stoppedState == StateSyncing
			},
			StartStateF: func(chainID ids.ID, state State) {
				startedState = state
			},
			StopStateF: func(chainID ids.ID, state State) {
				stoppedState = state
			},
			GetStateF: func(chainID ids.ID) State {
				return startedState
			},
		},
	}
}
