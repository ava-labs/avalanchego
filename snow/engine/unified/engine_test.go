// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unified_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/unified"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"

	mock "github.com/ava-labs/avalanchego/snow/engine/unified/mocks"
)

type mockStateSyncer struct {
	*enginetest.Engine
}

func (*mockStateSyncer) Clear(_ context.Context) error {
	return nil
}

func (*mockStateSyncer) IsEnabled(_ context.Context) (bool, error) {
	return true, nil
}

type engineStates []snow.EngineState

func (es engineStates) exclude(state snow.EngineState) []snow.EngineState {
	result := make([]snow.EngineState, 0, 6)
	for _, s := range es {
		if s == state {
			continue
		}
		result = append(result, s)
	}
	return result
}

func (es engineStates) contains(state snow.EngineState) bool {
	for _, s := range es {
		if s == state {
			return true
		}
	}

	return false
}

var engineStateSpace = engineStates{
	{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.StateSyncing,
	},
	{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.Bootstrapping,
	},
	{
		Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
		State: snow.NormalOp,
	},
	{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.StateSyncing,
	},
	{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.Bootstrapping,
	},
	{
		Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
		State: snow.NormalOp,
	},
}

func TestEngine(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		invoke             func(e *unified.Engine)
		setup              func(engine *enginetest.Engine, invoked *bool)
		state              snow.EngineState
		shouldBeIgnored    bool
		shouldBeRoutedToVM bool
		method             string
	}{
		{
			name: "pull query",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.PullQuery(context.Background(), ids.EmptyNodeID, 0, ids.Empty, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.PullQueryF = func(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, _ uint64) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "push query",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.PushQuery(context.Background(), ids.EmptyNodeID, 0, nil, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.PushQueryF = func(_ context.Context, _ ids.NodeID, _ uint32, _ []byte, _ uint64) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "put",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.Put(context.Background(), ids.EmptyNodeID, 0, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.PutF = func(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "get failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.GetFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GetFailedF = func(_ context.Context, _ ids.NodeID, _ uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "chits",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.Chits(context.Background(), ids.EmptyNodeID, 0, ids.Empty, ids.Empty, ids.Empty, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.ChitsF = func(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, _ ids.ID, _ ids.ID, _ uint64) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "query failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.QueryFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.QueryFailedF = func(_ context.Context, _ ids.NodeID, _ uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "gossip",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.Gossip(context.Background()))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GossipF = func(_ context.Context) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "state summary frontier",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.StateSummaryFrontier(context.Background(), ids.EmptyNodeID, 0, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.StateSummaryFrontierF = func(context.Context, ids.NodeID, uint32, []byte) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "get state summary frontier failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.GetStateSummaryFrontierFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GetStateSummaryFrontierFailedF = func(context.Context, ids.NodeID, uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "accepted state summary",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AcceptedStateSummary(context.Background(), ids.EmptyNodeID, 0, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AcceptedStateSummaryF = func(context.Context, ids.NodeID, uint32, set.Set[ids.ID]) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "get accepted state summary failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.GetAcceptedStateSummaryFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GetAcceptedStateSummaryFailedF = func(context.Context, ids.NodeID, uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "accepted frontier",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AcceptedFrontier(context.Background(), ids.EmptyNodeID, 0, ids.Empty))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "get accepted frontier failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.GetAcceptedFrontierFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GetAcceptedFrontierFailedF = func(_ context.Context, _ ids.NodeID, _ uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "timeout",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.Timeout(context.Background()))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.TimeoutF = func(_ context.Context) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "accepted",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.Accepted(context.Background(), ids.EmptyNodeID, 0, set.NewSet[ids.ID](0)))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, _ set.Set[ids.ID]) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name: "get accepted failed",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.GetAcceptedFailed(context.Background(), ids.EmptyNodeID, 0))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.GetAcceptedFailedF = func(_ context.Context, _ ids.NodeID, _ uint32) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name:               "app request",
			shouldBeRoutedToVM: true,
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AppRequest(context.Background(), ids.EmptyNodeID, 0, time.Time{}, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AppRequestF = func(_ context.Context, _ ids.NodeID, _ uint32, _ time.Time, _ []byte) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name:               "app gossip",
			shouldBeRoutedToVM: true,
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AppGossip(context.Background(), ids.EmptyNodeID, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AppGossipF = func(_ context.Context, _ ids.NodeID, _ []byte) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name:               "app response",
			shouldBeRoutedToVM: true,
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AppResponse(context.Background(), ids.EmptyNodeID, 0, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AppResponseF = func(_ context.Context, _ ids.NodeID, _ uint32, _ []byte) error {
					*invoked = true
					return nil
				}
			},
		},
		{
			name:               "app request failed",
			shouldBeRoutedToVM: true,
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_UNSPECIFIED,
				State: snow.NormalOp,
			},
			invoke: func(e *unified.Engine) {
				require.NoError(t, e.AppRequestFailed(context.Background(), ids.EmptyNodeID, 0, nil))
			},
			setup: func(e *enginetest.Engine, invoked *bool) {
				e.AppRequestFailedF = func(_ context.Context, _ ids.NodeID, _ uint32, _ *common.AppError) error {
					*invoked = true
					return nil
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)

			// Cartesian product of state and expectation
			stateToShouldBeIgnored := make(map[snow.EngineState]bool)

			// We take the complement of the state space defined in the test case,
			// and expect the inverse result.
			states := engineStateSpace.exclude(testCase.state)
			for _, state := range states {
				stateToShouldBeIgnored[state] = !testCase.shouldBeIgnored
			}
			// In the end, we also add the actual test case, but only if it's a valid state
			if engineStateSpace.contains(testCase.state) {
				stateToShouldBeIgnored[testCase.state] = testCase.shouldBeIgnored
			}

			for state, shouldBeIgnored := range stateToShouldBeIgnored {
				var et enginetest.Engine
				var invoked bool

				testCase.setup(&et, &invoked)

				var mockSS mockStateSyncer
				mockSS.Engine = &et

				ctrl := gomock.NewController(t)
				ef := mock.NewFactory(ctrl)
				ef.EXPECT().AllGetServer().Return(&et).AnyTimes()
				ef.EXPECT().NewAvalancheAncestorsGetter().Return(&et).AnyTimes()
				ef.EXPECT().NewSnowman().Return(&et, nil).AnyTimes()
				ef.EXPECT().NewStateSyncer(gomock.Any()).Return(&mockSS, nil).AnyTimes()
				ef.EXPECT().NewAvalancheSyncer(gomock.Any()).Return(&mockSS, nil).AnyTimes()
				ef.EXPECT().NewSnowBootstrapper(gomock.Any()).Return(&mockSS, nil).AnyTimes()
				ef.EXPECT().ClearBootstrapDB().Return(nil).AnyTimes()

				snowCtx := snowtest.Context(t, snowtest.CChainID)
				ctx := snowtest.ConsensusContext(snowCtx)
				ctx.State.Set(state)

				var vm enginetest.VM
				routedToVM := configureVMRouting(&vm)

				engine, err := unified.EngineFromEngines(ctx, ef, &vm)
				require.NoError(err)

				require.NoError(engine.Start(context.Background(), 0))

				testCase.invoke(engine)

				fmt.Println(shouldBeIgnored, state)

				ignored := !invoked
				require.Equal(shouldBeIgnored, ignored)
				require.Equal(testCase.shouldBeRoutedToVM, *routedToVM)
			}
		})
	}
}

func TestEngineDispatch(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		method          string
		instance        string
		state           snow.EngineState
		shouldBeIgnored bool
	}{
		{
			name:     "ancestors x avalanche",
			method:   "Ancestors",
			instance: "avalanche",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "ancestors x bootstrapper",
			method:   "Ancestors",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "ancestors x state sync",
			method:   "Ancestors",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			shouldBeIgnored: true,
		},
		{
			name:     "getAncestorsFailed x avalanche",
			method:   "GetAncestorsFailed",
			instance: "avalanche",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_AVALANCHE,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "getAncestorsFailed x bootstrapper",
			method:   "GetAncestorsFailed",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "getAncestorsFailed x state sync",
			method:   "GetAncestorsFailed",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
			shouldBeIgnored: true,
		},
		{
			name:     "connected x bootstrapper",
			method:   "Connected",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "connected x state-syncer",
			method:   "Connected",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
		},
		{
			name:     "connected x snowman",
			method:   "Connected",
			instance: "snowman",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
		},
		{
			name:     "connected x avalanche",
			method:   "Connected",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
		{
			name:     "disconnected x bootstrapper",
			method:   "Disconnected",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "disconnected x state-syncer",
			method:   "Disconnected",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
		},
		{
			name:     "disconnected x snowman",
			method:   "Disconnected",
			instance: "snowman",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
		},
		{
			name:     "disconnected x avalanche",
			method:   "Disconnected",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
		{
			name:     "notify x bootstrapper",
			method:   "Notify",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "notify x state-syncer",
			method:   "Notify",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
		},
		{
			name:     "notify x snowman",
			method:   "Notify",
			instance: "snowman",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
		},
		{
			name:     "notify x avalanche",
			method:   "Notify",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
		{
			name:     "healthcheck x bootstrapper",
			method:   "HealthCheck",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "healthcheck x state-syncer",
			method:   "HealthCheck",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
		},
		{
			name:     "healthcheck x snowman",
			method:   "HealthCheck",
			instance: "snowman",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
		},
		{
			name:     "healthcheck x avalanche",
			method:   "HealthCheck",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
		{
			name:     "getAncestors x avalanche",
			method:   "GetAncestors(avalanche)",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
		{
			name:     "getAncestors x avalanche",
			method:   "GetAncestors(snowman)",
			instance: "all-get-server",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "shutdown x bootstrapper",
			method:   "Shutdown",
			instance: "bootstrapper",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.Bootstrapping,
			},
		},
		{
			name:     "shutdown x state-syncer",
			method:   "Shutdown",
			instance: "state-syncer",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.StateSyncing,
			},
		},
		{
			name:     "shutdown x snowman",
			method:   "Shutdown",
			instance: "snowman",
			state: snow.EngineState{
				Type:  p2p.EngineType_ENGINE_TYPE_SNOWMAN,
				State: snow.NormalOp,
			},
		},
		{
			name:     "shutdown x avalanche",
			method:   "Shutdown",
			instance: "avalanche",
			state: snow.EngineState{
				Type: p2p.EngineType_ENGINE_TYPE_AVALANCHE,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var sm enginetest.Engine
			var ss enginetest.Engine
			var bs enginetest.Engine
			var as enginetest.Engine
			var gs enginetest.Engine

			var getServer mockStateSyncer
			getServer.Engine = &gs

			var snowman mockStateSyncer
			snowman.Engine = &sm

			var stateSyncer mockStateSyncer
			stateSyncer.Engine = &ss

			var bootstrapper mockStateSyncer
			bootstrapper.Engine = &bs

			var avalancheSyncer mockStateSyncer
			avalancheSyncer.Engine = &as

			// {avalanche | bootstrapper | state-syncer | snowman} x method --> struct{}
			// registers that a method has been invoked on a given backend instance
			invokedTable := setupInvocationTables(avalancheSyncer, bootstrapper, stateSyncer, snowman, getServer)

			ef := createEngineFactory(t, gs, as, sm, stateSyncer, avalancheSyncer, bootstrapper)

			snowCtx := snowtest.Context(t, snowtest.CChainID)
			ctx := snowtest.ConsensusContext(snowCtx)
			ctx.State.Set(testCase.state)

			var vm enginetest.VM
			configureVMRouting(&vm)

			engine, err := unified.EngineFromEngines(ctx, ef, &vm)
			require.NoError(t, err)

			// method --> func(*unified.Engine),
			// executes a requested method of the engine instance
			invocationMap := createEngineInvocationMap(t)

			require.NoError(t, engine.Start(context.Background(), 0))

			// Ensure invoked table is empty before invocation
			require.Empty(t, invokedTable)
			// Invoke the method as registered in the invocation map
			invocationMap[testCase.method](engine)
			// Ensure it was routed to the correct engine unless it should have been ignored
			if testCase.shouldBeIgnored {
				require.Empty(t, invokedTable)
			} else {
				_, ok := invokedTable[testCase.instance+testCase.method]
				require.True(t, ok)
			}
		})
	}
}

func setupInvocationTables(avalancheSyncer mockStateSyncer, bootstrapper mockStateSyncer, stateSyncer mockStateSyncer, snowman mockStateSyncer, getServer mockStateSyncer) map[string]struct{} {
	invokedTable := map[string]struct{}{}
	setupInvokeTable(&avalancheSyncer, invokedTable, "avalanche")
	setupInvokeTable(&bootstrapper, invokedTable, "bootstrapper")
	setupInvokeTable(&stateSyncer, invokedTable, "state-syncer")
	setupInvokeTable(&snowman, invokedTable, "snowman")
	setupInvokeTable(&getServer, invokedTable, "all-get-server")
	return invokedTable
}

func createEngineFactory(t *testing.T, gs enginetest.Engine, as enginetest.Engine, sm enginetest.Engine, stateSyncer mockStateSyncer, avalancheSyncer mockStateSyncer, bootstrapper mockStateSyncer) *mock.Factory {
	ctrl := gomock.NewController(t)
	ef := mock.NewFactory(ctrl)
	ef.EXPECT().AllGetServer().Return(&gs).AnyTimes()
	ef.EXPECT().NewAvalancheAncestorsGetter().Return(&as).AnyTimes()
	ef.EXPECT().NewSnowman().Return(&sm, nil).AnyTimes()
	ef.EXPECT().NewStateSyncer(gomock.Any()).Return(&stateSyncer, nil).AnyTimes()
	ef.EXPECT().NewAvalancheSyncer(gomock.Any()).Return(&avalancheSyncer, nil).AnyTimes()
	ef.EXPECT().NewSnowBootstrapper(gomock.Any()).Return(&bootstrapper, nil).AnyTimes()
	ef.EXPECT().ClearBootstrapDB().Return(nil).AnyTimes()
	return ef
}

func createEngineInvocationMap(t *testing.T) map[string]func(*unified.Engine) {
	m := make(map[string]func(*unified.Engine))

	m["Ancestors"] = func(e *unified.Engine) {
		require.NoError(t, e.Ancestors(context.Background(), ids.EmptyNodeID, 0, nil))
	}

	m["GetAncestorsFailed"] = func(e *unified.Engine) {
		require.NoError(t, e.GetAncestorsFailed(context.Background(), ids.EmptyNodeID, 0))
	}

	m["Connected"] = func(e *unified.Engine) {
		require.NoError(t, e.Connected(context.Background(), ids.EmptyNodeID, nil))
	}

	m["Disconnected"] = func(e *unified.Engine) {
		require.NoError(t, e.Disconnected(context.Background(), ids.EmptyNodeID))
	}

	m["Notify"] = func(e *unified.Engine) {
		require.NoError(t, e.Notify(context.Background(), 0))
	}

	m["HealthCheck"] = func(e *unified.Engine) {
		_, err := e.HealthCheck(context.Background())
		require.NoError(t, err)
	}

	m["GetAncestors(avalanche)"] = func(e *unified.Engine) {
		require.NoError(t, e.GetAncestors(context.Background(), ids.EmptyNodeID, 0, ids.Empty, p2p.EngineType_ENGINE_TYPE_AVALANCHE))
	}

	m["GetAncestors(snowman)"] = func(e *unified.Engine) {
		require.NoError(t, e.GetAncestors(context.Background(), ids.EmptyNodeID, 0, ids.Empty, p2p.EngineType_ENGINE_TYPE_SNOWMAN))
	}

	m["Shutdown"] = func(e *unified.Engine) {
		require.NoError(t, e.Shutdown(context.Background()))
	}

	return m
}

func setupInvokeTable(ss *mockStateSyncer, invokeTable map[string]struct{}, instance string) {
	ss.AncestorsF = func(context.Context, ids.NodeID, uint32, [][]byte) error {
		invokeTable[instance+"Ancestors"] = struct{}{}
		return nil
	}

	ss.GetAncestorsFailedF = func(context.Context, ids.NodeID, uint32) error {
		invokeTable[instance+"GetAncestorsFailed"] = struct{}{}
		return nil
	}

	ss.ConnectedF = func(context.Context, ids.NodeID, *version.Application) error {
		invokeTable[instance+"Connected"] = struct{}{}
		return nil
	}

	ss.DisconnectedF = func(context.Context, ids.NodeID) error {
		invokeTable[instance+"Disconnected"] = struct{}{}
		return nil
	}

	ss.NotifyF = func(context.Context, common.Message) error {
		invokeTable[instance+"Notify"] = struct{}{}
		return nil
	}

	ss.HealthF = func(context.Context) (interface{}, error) {
		invokeTable[instance+"HealthCheck"] = struct{}{}
		return nil, nil
	}

	ss.GetAncestorsF = func(_ context.Context, _ ids.NodeID, _ uint32, _ ids.ID, engineType p2p.EngineType) error {
		switch engineType {
		case p2p.EngineType_ENGINE_TYPE_SNOWMAN:
			invokeTable[instance+"GetAncestors(snowman)"] = struct{}{}
		case p2p.EngineType_ENGINE_TYPE_AVALANCHE:
			invokeTable[instance+"GetAncestors(avalanche)"] = struct{}{}
		}
		return nil
	}

	ss.ShutdownF = func(context.Context) error {
		invokeTable[instance+"Shutdown"] = struct{}{}
		return nil
	}
}

func configureVMRouting(vm *enginetest.VM) *bool {
	var routedToVM bool
	vm.AppRequestF = func(context.Context, ids.NodeID, uint32, time.Time, []byte) error {
		routedToVM = true
		return nil
	}

	vm.AppRequestFailedF = func(context.Context, ids.NodeID, uint32, *common.AppError) error {
		routedToVM = true
		return nil
	}

	vm.AppGossipF = func(context.Context, ids.NodeID, []byte) error {
		routedToVM = true
		return nil
	}

	vm.AppResponseF = func(context.Context, ids.NodeID, uint32, []byte) error {
		routedToVM = true
		return nil
	}
	return &routedToVM
}
