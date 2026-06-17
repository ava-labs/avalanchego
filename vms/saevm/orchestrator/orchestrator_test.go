// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, saetest.GoleakOptions()...)
}

func TestShutdownBeforeInitialize(t *testing.T) {
	o := NewStateSyncable[adaptor.SummaryProperties](nil, nil, nil)
	require.NoError(t, o.Shutdown(t.Context()))
}

// TestWaitForEventRoutingDeterministic asserts that, in every pre-[snow.NormalOp]
// state, [orchestrator.WaitForEvent] parks in the [SummaryHandler] and never
// reaches the [ChainVM]; and that once in [snow.NormalOp] it routes to the
// [ChainVM] (which by then has been initialized).
//
// [testing/synctest] makes the otherwise-flaky "is the call parked?" assertion
// deterministic: synctest.Wait blocks until every goroutine in the bubble is
// durably blocked, so an empty result channel after Wait proves the call is
// genuinely parked rather than merely slow.
func TestWaitForEventRoutingDeterministic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		chain := newStubChainVM(t)
		summary := newStubSummaryHandler()
		o := NewStateSyncable[adaptor.SummaryProperties](chain, nil, summary)
		ctx := t.Context()

		for _, state := range []snow.State{snow.Initializing, snow.StateSyncing, snow.Bootstrapping} {
			require.NoErrorf(t, o.SetState(ctx, state), "SetState(%s)", state)

			done := goWaitForEvent(o, ctx)
			synctest.Wait()
			require.Emptyf(t, done, "WaitForEvent should be parked in the summary handler at %s", state)
			require.Zerof(t, chain.waitCalls.Load(), "ChainVM.WaitForEvent must not be called at %s", state)

			summary.release <- struct{}{}
			synctest.Wait()
			r := <-done
			require.NoErrorf(t, r.err, "WaitForEvent() at %s", state)
			require.Equalf(t, common.StateSyncDone, r.msg, "WaitForEvent() should route to summary handler at %s", state)
		}

		require.NoError(t, o.SetState(ctx, snow.NormalOp))
		require.True(t, chain.initialized, "ChainVM must be initialized before NormalOp routing")

		done := goWaitForEvent(o, ctx)
		synctest.Wait()
		require.Empty(t, done, "WaitForEvent should be parked in the chain VM at NormalOp")

		chain.release <- struct{}{}
		synctest.Wait()
		r := <-done
		require.NoError(t, r.err, "WaitForEvent() at NormalOp")
		require.Equal(t, common.PendingTxs, r.msg, "WaitForEvent() should route to chain VM at NormalOp")
	})
}

// TestWaitForEventConcurrentWithSetState is the core guard: it models the
// engine calling [orchestrator.WaitForEvent] on its own goroutine concurrently
// with [orchestrator.SetState] driving the VM all the way to [snow.NormalOp]
// (which lazily builds the [ChainVM]). It asserts that an in-flight pre-NormalOp
// call never escalates to the chain mid-flight, and that only a fresh call made
// once in NormalOp routes to the chain.
func TestWaitForEventConcurrentWithSetState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		chain := newStubChainVM(t)
		summary := newStubSummaryHandler()
		o := NewStateSyncable[adaptor.SummaryProperties](chain, nil, summary)
		ctx := t.Context()

		// Start a WaitForEvent while still Initializing; it parks in the summary handler.
		done := goWaitForEvent(o, ctx)
		synctest.Wait()
		require.Empty(t, done, "WaitForEvent should be parked in the summary handler")

		// Drive the state forward underneath the in-flight call. The transition
		// into Bootstrapping builds the ChainVM; NormalOp enables chain routing.
		for _, state := range []snow.State{snow.StateSyncing, snow.Bootstrapping, snow.NormalOp} {
			require.NoErrorf(t, o.SetState(ctx, state), "SetState(%s)", state)
			synctest.Wait()
			require.Emptyf(t, done, "in-flight WaitForEvent must not return as state advances to %s", state)
			require.Zerof(t, chain.waitCalls.Load(), "in-flight WaitForEvent must not escalate to the chain at %s", state)
		}

		// Releasing the summary handler proves the in-flight call stayed there
		// the whole time, never escalating to the chain.
		summary.release <- struct{}{}
		synctest.Wait()
		r := <-done
		require.NoError(t, r.err, "in-flight WaitForEvent()")
		require.Equal(t, common.StateSyncDone, r.msg, "in-flight call should have stayed in the summary handler")

		// A fresh call, now that state == NormalOp, routes to the chain.
		done = goWaitForEvent(o, ctx)
		synctest.Wait()
		require.Empty(t, done, "fresh WaitForEvent should be parked in the chain VM at NormalOp")
		chain.release <- struct{}{}
		synctest.Wait()
		r = <-done
		require.NoError(t, r.err, "fresh WaitForEvent() at NormalOp")
		require.Equal(t, common.PendingTxs, r.msg, "fresh WaitForEvent() should route to the chain VM at NormalOp")
	})
}

type waitResult struct {
	msg common.Message
	err error
}

// goWaitForEvent runs o.WaitForEvent on its own goroutine, mirroring how the
// engine calls it, and delivers the result on a buffered channel.
func goWaitForEvent(o adaptor.ChainVMWithContext, ctx context.Context) chan waitResult {
	done := make(chan waitResult, 1)
	go func() {
		msg, err := o.WaitForEvent(ctx)
		done <- waitResult{msg: msg, err: err}
	}()
	return done
}

// stubChainVM is a minimal [ChainVM]. Its WaitForEvent returns [common.PendingTxs]
// once released, and records both an invocation count and — as the backstop for
// the "too early" invariant — a test failure if it is ever entered before
// Initialize has returned.
type stubChainVM struct {
	t           *testing.T
	initialized bool
	waitCalls   atomic.Int64
	release     chan struct{}
}

func newStubChainVM(t *testing.T) *stubChainVM {
	return &stubChainVM{t: t, release: make(chan struct{})}
}

func (c *stubChainVM) Initialize(context.Context, *snow.Context, database.Database, []byte, []byte, *network.Network) error {
	c.initialized = true
	return nil
}

func (c *stubChainVM) WaitForEvent(ctx context.Context) (common.Message, error) {
	c.waitCalls.Add(1)
	if !c.initialized {
		c.t.Errorf("ChainVM.WaitForEvent called before Initialize returned (routed too early)")
	}
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-c.release:
		return common.PendingTxs, nil
	}
}

func (*stubChainVM) HealthCheck(context.Context) (interface{}, error)          { return nil, nil }
func (*stubChainVM) SetState(context.Context, snow.State) error                { return nil }
func (*stubChainVM) Shutdown(context.Context) error                            { return nil }
func (*stubChainVM) NewHTTPHandler(context.Context) (http.Handler, error)      { return nil, nil }
func (*stubChainVM) ParseBlock(context.Context, []byte) (*blocks.Block, error) { return nil, nil }
func (*stubChainVM) BuildBlock(context.Context, *block.Context) (*blocks.Block, error) {
	return nil, nil
}
func (*stubChainVM) VerifyBlock(context.Context, *block.Context, *blocks.Block) error { return nil }
func (*stubChainVM) AcceptBlock(context.Context, *blocks.Block) error                 { return nil }
func (*stubChainVM) RejectBlock(context.Context, *blocks.Block) error                 { return nil }
func (*stubChainVM) SetPreference(context.Context, ids.ID, *block.Context) error      { return nil }
func (*stubChainVM) GetBlock(context.Context, ids.ID) (*blocks.Block, error)          { return nil, nil }
func (*stubChainVM) LastAccepted(context.Context) (ids.ID, error)                     { return ids.Empty, nil }
func (*stubChainVM) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, nil
}
func (*stubChainVM) Version(context.Context) (string, error)                         { return "", nil }
func (*stubChainVM) CreateHandlers(context.Context) (map[string]http.Handler, error) { return nil, nil }

// stubSummaryHandler is a minimal [SummaryHandler]. Its WaitForEvent returns
// [common.StateSyncDone] once released so the test can distinguish the summary
// route from the chain route.
type stubSummaryHandler struct {
	release chan struct{}
}

func newStubSummaryHandler() *stubSummaryHandler {
	return &stubSummaryHandler{release: make(chan struct{})}
}

func (h *stubSummaryHandler) WaitForEvent(ctx context.Context) (common.Message, error) {
	select {
	case <-ctx.Done():
		return common.StateSyncDone, ctx.Err()
	case <-h.release:
		return common.StateSyncDone, nil
	}
}

func (*stubSummaryHandler) Initialize(context.Context, *snow.Context, StateSyncConfig, database.Database, *types.Block) error {
	return nil
}
func (*stubSummaryHandler) GetBlock(context.Context, ids.ID) (*blocks.Block, error) { return nil, nil }
func (*stubSummaryHandler) LastAccepted(context.Context) (ids.ID, error)            { return ids.Empty, nil }
func (*stubSummaryHandler) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, nil
}
func (*stubSummaryHandler) Shutdown(context.Context) error                 { return nil }
func (*stubSummaryHandler) StateSyncEnabled(context.Context) (bool, error) { return false, nil }
func (*stubSummaryHandler) GetLastStateSummary(context.Context) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*stubSummaryHandler) GetOngoingSyncStateSummary(context.Context) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*stubSummaryHandler) GetStateSummary(context.Context, uint64) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*stubSummaryHandler) ParseStateSummary(context.Context, []byte) (adaptor.SummaryProperties, error) {
	return nil, nil
}
func (*stubSummaryHandler) AcceptSummary(context.Context, adaptor.SummaryProperties) (block.StateSyncMode, error) {
	return block.StateSyncSkipped, nil
}
