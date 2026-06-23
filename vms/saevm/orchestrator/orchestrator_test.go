// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
)

func TestInterfaces(t *testing.T) {
	t.Run("New()", func(t *testing.T) {
		o := New(nil)
		_, ok := o.(block.StateSyncableVM)
		require.Falsef(t, ok, "New() should not return a block.StateSynableVM")
	})

	t.Run("NewStateSyncable()", func(t *testing.T) {
		o := NewStateSyncable[adaptor.SummaryProperties](nil, nil, nil)
		_, ok := o.(block.StateSyncableVM)
		require.Truef(t, ok, "NewStateSyncable() should return a block.StateSyncableVM")
	})
}

func mustInitialize(t *testing.T, o adaptor.ChainVMWithContext) {
	t.Helper()

	err := o.Initialize(t.Context(), snowtest.Context(t, ids.ID{}), nil, nil, nil, nil, nil, &enginetest.SenderStub{})
	require.NoErrorf(t, err, "%T.Initialize()", o)
}

func TestShutdownAnyState(t *testing.T) {
	for _, state := range []snow.State{snow.Initializing, snow.StateSyncing, snow.Bootstrapping, snow.NormalOp} {
		t.Run(state.String(), func(t *testing.T) {
			chain := &spyChainVM{}
			summary := &spySummaryHandler{}
			o := NewStateSyncable[adaptor.SummaryProperties](chain, summary, parser{})
			if state != snow.Initializing {
				mustInitialize(t, o)
				require.NoErrorf(t, o.SetState(t.Context(), state), "SetState(%s)", state)
			}
			require.NoErrorf(t, o.Shutdown(t.Context()), "Shutdown() at %s", state)
		})
	}
}

type routedMethod int

const (
	noMethod routedMethod = iota // zero value
	getBlockMethod
	getBlockIDAtHeightMethod
	lastAcceptedMethod
	waitForEventMethod
)

func (m routedMethod) String() string {
	switch m {
	case getBlockMethod:
		return "GetBlock"
	case getBlockIDAtHeightMethod:
		return "GetBlockIDAtHeight"
	case lastAcceptedMethod:
		return "LastAccepted"
	case waitForEventMethod:
		return "WaitForEvent"
	default:
		return "none"
	}
}

// routeRecorder captures the routed method a stub most recently received, so a
// test can observe which handler the orchestrator selected.
type routeRecorder struct {
	method routedMethod
}

func (r *routeRecorder) record(m routedMethod) { r.method = m }

// take returns the recorded method and resets the recorder to [noMethod].
func (r *routeRecorder) take() routedMethod {
	m := r.method
	r.method = noMethod
	return m
}

func TestStateRouting(t *testing.T) {
	chain := &spyChainVM{}
	summary := &spySummaryHandler{}
	o := NewStateSyncable[adaptor.SummaryProperties](chain, summary, parser{})
	mustInitialize(t, o)
	ctx := t.Context()

	routedMethods := []struct {
		method routedMethod
		call   func()
	}{
		{
			method: getBlockMethod,
			call:   func() { _, _ = o.GetBlock(ctx, ids.Empty) },
		},
		{
			method: getBlockIDAtHeightMethod,
			call:   func() { _, _ = o.GetBlockIDAtHeight(ctx, 0) },
		},
		{
			method: lastAcceptedMethod,
			call:   func() { _, _ = o.LastAccepted(ctx) },
		},
		{
			method: waitForEventMethod,
			call:   func() { _, _ = o.WaitForEvent(ctx) },
		},
	}
	states := []struct {
		state   snow.State
		toChain bool
	}{
		{snow.Initializing, false},
		{snow.StateSyncing, false},
		{snow.Bootstrapping, true},
		{snow.NormalOp, true},
	}

	for _, st := range states {
		t.Run(st.state.String(), func(t *testing.T) {
			require.NoErrorf(t, o.SetState(ctx, st.state), "SetState(%s)", st.state)
			for _, m := range routedMethods {
				t.Run(m.method.String(), func(t *testing.T) {
					m.call()

					gotChain, gotSummary := chain.routed.take(), summary.routed.take()
					if st.toChain {
						require.Equal(t, m.method, gotChain, "should route to ChainVM")
						require.Equal(t, noMethod, gotSummary, "should not reach SummaryHandler")
					} else {
						require.Equal(t, m.method, gotSummary, "should route to SummaryHandler")
						require.Equal(t, noMethod, gotChain, "should not reach ChainVM")
					}
				})
			}
		})
	}
}

// spyChainVM tracks the most recent routed method it received
type spyChainVM struct {
	routed routeRecorder
}

func (c *spyChainVM) Initialize(context.Context, *snow.Context, database.Database, []byte, []byte, *network.Network) error {
	return nil
}

func (c *spyChainVM) WaitForEvent(context.Context) (common.Message, error) {
	c.routed.record(waitForEventMethod)
	return common.PendingTxs, nil
}

func (c *spyChainVM) GetBlock(context.Context, ids.ID) (*blocks.Block, error) {
	c.routed.record(getBlockMethod)
	return nil, nil
}
func (c *spyChainVM) LastAccepted(context.Context) (ids.ID, error) {
	c.routed.record(lastAcceptedMethod)
	return ids.Empty, nil
}
func (c *spyChainVM) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	c.routed.record(getBlockIDAtHeightMethod)
	return ids.Empty, nil
}

func (*spyChainVM) HealthCheck(context.Context) (interface{}, error)          { return nil, nil }
func (*spyChainVM) SetState(context.Context, snow.State) error                { return nil }
func (*spyChainVM) Shutdown(context.Context) error                            { return nil }
func (*spyChainVM) NewHTTPHandler(context.Context) (http.Handler, error)      { return nil, nil }
func (*spyChainVM) ParseBlock(context.Context, []byte) (*blocks.Block, error) { return nil, nil }
func (*spyChainVM) BuildBlock(context.Context, *block.Context) (*blocks.Block, error) {
	return nil, nil
}
func (*spyChainVM) VerifyBlock(context.Context, *block.Context, *blocks.Block) error { return nil }
func (*spyChainVM) AcceptBlock(context.Context, *blocks.Block) error                 { return nil }
func (*spyChainVM) RejectBlock(context.Context, *blocks.Block) error                 { return nil }
func (*spyChainVM) SetPreference(context.Context, ids.ID, *block.Context) error      { return nil }

func (*spyChainVM) Version(context.Context) (string, error)                         { return "", nil }
func (*spyChainVM) CreateHandlers(context.Context) (map[string]http.Handler, error) { return nil, nil }

// spySummaryHandler tracks the most recent routed method it received
type spySummaryHandler struct {
	routed routeRecorder
}

func (h *spySummaryHandler) WaitForEvent(context.Context) (common.Message, error) {
	h.routed.record(waitForEventMethod)
	return common.StateSyncDone, nil
}

func (*spySummaryHandler) Initialize(context.Context, *snow.Context, StateSyncConfig, database.Database, *types.Block) error {
	return nil
}

func (h *spySummaryHandler) GetBlock(context.Context, ids.ID) (*blocks.Block, error) {
	h.routed.record(getBlockMethod)
	return nil, nil
}

func (h *spySummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	h.routed.record(lastAcceptedMethod)
	return ids.Empty, nil
}

func (h *spySummaryHandler) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	h.routed.record(getBlockIDAtHeightMethod)
	return ids.Empty, nil
}

func (*spySummaryHandler) Shutdown(context.Context) error                 { return nil }
func (*spySummaryHandler) StateSyncEnabled(context.Context) (bool, error) { return false, nil }
func (*spySummaryHandler) GetLastStateSummary(context.Context) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*spySummaryHandler) GetOngoingSyncStateSummary(context.Context) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*spySummaryHandler) GetStateSummary(context.Context, uint64) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*spySummaryHandler) ParseStateSummary(context.Context, []byte) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*spySummaryHandler) AcceptSummary(context.Context, adaptor.SummaryProperties) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, nil
}

type parser struct{}

func (parser) ParseGenesis([]byte) (*types.Block, error) { return nil, nil }
func (parser) ParseConfig([]byte) (VMConfig, error)      { return config{}, nil }

type config struct{}

func (config) StateSyncConfig() StateSyncConfig { return StateSyncConfig{} }
