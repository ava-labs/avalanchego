// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
)

var (
	errChain   = errors.New("from chain")
	errHandler = errors.New("from handler")
	errBuild   = errors.New("build failed")
)

type fakeSummary struct{}

func (fakeSummary) ID() ids.ID     { return ids.Empty }
func (fakeSummary) Bytes() []byte  { return nil }
func (fakeSummary) Height() uint64 { return 0 }

var _ adaptor.SummaryProperties = fakeSummary{}

// fakeChainVM records what the orchestrator does to the chain. The read-only
// methods return sentinels and are safe for concurrent use, the recording ones
// are guarded by mu. The embedded ChainVM supplies the methods the tests do not
// exercise, so an unexpected call panics rather than passing silently.
type fakeChainVM struct {
	ChainVM

	mu                   sync.Mutex
	initCount            int
	shutdownCount        int
	forwarded            []snow.State // states forwarded via SetState
	forwardedBeforeBuild bool         // a SetState arrived before the build
	buildErr             error        // returned by the build
	buildCtxErr          error        // ctx.Err() observed by the build
}

var _ ChainVM = (*fakeChainVM)(nil)

func (f *fakeChainVM) Initialize(ctx context.Context, _ *snow.Context, _ database.Database, _ []byte, _ VMConfig, _ *network.Network) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.initCount++
	f.buildCtxErr = ctx.Err()
	return f.buildErr
}

func (f *fakeChainVM) SetState(_ context.Context, s snow.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.initCount == 0 {
		f.forwardedBeforeBuild = true
	}
	f.forwarded = append(f.forwarded, s)
	return nil
}

func (f *fakeChainVM) Shutdown(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCount++
	return nil
}

func (*fakeChainVM) WaitForEvent(context.Context) (common.Message, error) {
	return common.PendingTxs, nil
}
func (*fakeChainVM) GetBlock(context.Context, ids.ID) (*blocks.Block, error) { return nil, errChain }
func (*fakeChainVM) LastAccepted(context.Context) (ids.ID, error)            { return ids.Empty, errChain }
func (*fakeChainVM) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, errChain
}

// fakeSummaryHandler answers the routing-relevant methods with sentinels and
// counts its shutdowns. The embedded interface supplies the rest.
type fakeSummaryHandler struct {
	SummaryHandler[fakeSummary]

	shutdownCount int
}

var _ SummaryHandler[fakeSummary] = (*fakeSummaryHandler)(nil)

func (h *fakeSummaryHandler) Shutdown(context.Context) error { h.shutdownCount++; return nil }

func (*fakeSummaryHandler) GetBlock(context.Context, ids.ID) (*blocks.Block, error) {
	return nil, errHandler
}

func (*fakeSummaryHandler) LastAccepted(context.Context) (ids.ID, error) {
	return ids.Empty, errHandler
}

func (*fakeSummaryHandler) GetBlockIDAtHeight(context.Context, uint64) (ids.ID, error) {
	return ids.Empty, errHandler
}

func (*fakeSummaryHandler) WaitForEvent(context.Context) (common.Message, error) {
	return common.StateSyncDone, nil
}

func newSUT(chain *fakeChainVM, handler *fakeSummaryHandler) *Orchestrator[fakeSummary] {
	return &Orchestrator[fakeSummary]{ChainVM: chain, SummaryHandler: handler}
}

func built(o *Orchestrator[fakeSummary]) bool {
	_, _, ok := o.snapshot()
	return ok
}

func TestSetState_Build(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		transitions []snow.State
		wantBuilds  int
		wantForward []snow.State
	}{
		{
			name:        "syncing never builds",
			transitions: []snow.State{snow.StateSyncing},
		},
		{
			name:        "no-sync path builds on bootstrapping",
			transitions: []snow.State{snow.Bootstrapping},
			wantBuilds:  1,
			wantForward: []snow.State{snow.Bootstrapping},
		},
		{
			name:        "builds once across the lifecycle",
			transitions: []snow.State{snow.StateSyncing, snow.Bootstrapping, snow.NormalOp, snow.Bootstrapping},
			wantBuilds:  1,
			wantForward: []snow.State{snow.Bootstrapping, snow.NormalOp, snow.Bootstrapping},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chain := &fakeChainVM{}
			o := newSUT(chain, &fakeSummaryHandler{})

			for _, s := range tt.transitions {
				require.NoError(t, o.SetState(t.Context(), s))
			}

			require.Equal(t, tt.wantBuilds, chain.initCount)
			require.Equal(t, tt.wantForward, chain.forwarded)
			require.False(t, chain.forwardedBeforeBuild, "SetState forwarded before the chain was built")
			require.Equal(t, tt.wantBuilds > 0, built(o))
		})
	}
}

func TestSetState_BuildErrorRetries(t *testing.T) {
	t.Parallel()
	chain := &fakeChainVM{buildErr: errBuild}
	o := newSUT(chain, &fakeSummaryHandler{})

	require.ErrorIs(t, o.SetState(t.Context(), snow.Bootstrapping), errBuild)
	require.False(t, built(o))
	require.Empty(t, chain.forwarded, "SetState forwarded despite a failed build")

	chain.buildErr = nil
	require.NoError(t, o.SetState(t.Context(), snow.Bootstrapping))
	require.Equal(t, 2, chain.initCount, "the failed build was not retried")
	require.True(t, built(o))
}

func TestSetState_BuildUsesLiveCtx(t *testing.T) {
	t.Parallel()
	chain := &fakeChainVM{}
	o := newSUT(chain, &fakeSummaryHandler{})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, o.SetState(ctx, snow.Bootstrapping))
	require.ErrorIs(t, chain.buildCtxErr, context.Canceled, "the build did not receive the live SetState ctx")
}

// TestGetters_RouteByBuildState covers the three methods the snowman getter calls
// regardless of lifecycle state: they answer from the handler before the build
// and from the chain after it.
func TestGetters_RouteByBuildState(t *testing.T) {
	t.Parallel()
	o := newSUT(&fakeChainVM{}, &fakeSummaryHandler{})
	ctx := t.Context()

	requireGettersServedBy(ctx, t, o, errHandler)

	require.NoError(t, o.SetState(ctx, snow.Bootstrapping))
	requireGettersServedBy(ctx, t, o, errChain)
}

// requireGettersServedBy asserts that all three getter methods route to the
// collaborator returning want.
func requireGettersServedBy(ctx context.Context, t *testing.T, o *Orchestrator[fakeSummary], want error) {
	t.Helper()
	_, err := o.GetBlock(ctx, ids.Empty)
	require.ErrorIs(t, err, want)
	_, err = o.LastAccepted(ctx)
	require.ErrorIs(t, err, want)
	_, err = o.GetBlockIDAtHeight(ctx, 0)
	require.ErrorIs(t, err, want)
}

func TestWaitForEvent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   []snow.State
		cancel  bool
		wantMsg common.Message
		wantErr error
	}{
		{
			name:    "initializing blocks until cancelled",
			cancel:  true,
			wantErr: context.Canceled,
		},
		{
			name:    "syncing returns sync completion",
			setup:   []snow.State{snow.StateSyncing},
			wantMsg: common.StateSyncDone,
		},
		{
			name:    "built returns chain events",
			setup:   []snow.State{snow.Bootstrapping},
			wantMsg: common.PendingTxs,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := newSUT(&fakeChainVM{}, &fakeSummaryHandler{})
			for _, s := range tt.setup {
				require.NoError(t, o.SetState(t.Context(), s))
			}

			ctx := t.Context()
			if tt.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			msg, err := o.WaitForEvent(ctx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMsg, msg)
		})
	}
}

func TestShutdown(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		build             bool
		wantChainShutdown int
	}{
		{"before build closes only the handler", false, 0},
		{"after build closes both", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			chain := &fakeChainVM{}
			handler := &fakeSummaryHandler{}
			o := newSUT(chain, handler)
			if tt.build {
				require.NoError(t, o.SetState(t.Context(), snow.Bootstrapping))
			}

			require.NoError(t, o.Shutdown(t.Context()))
			require.Equal(t, 1, handler.shutdownCount, "the handler is always closed")
			require.Equal(t, tt.wantChainShutdown, chain.shutdownCount)
		})
	}
}

// TestSetState_InstallsChainBeforeAdvanceUnderRace runs concurrent reads across
// the bootstrapping transition. Routing keys on the built flag set under the
// lock, so a reader never observes an advanced state with no chain installed.
func TestSetState_InstallsChainBeforeAdvanceUnderRace(t *testing.T) {
	t.Parallel()
	chain := &fakeChainVM{}
	o := newSUT(chain, &fakeSummaryHandler{})

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, _ = o.WaitForEvent(ctx)
			_, _ = o.LastAccepted(t.Context())
		})
	}
	require.NoError(t, o.SetState(t.Context(), snow.Bootstrapping))
	wg.Wait()

	require.True(t, built(o))
	require.False(t, chain.forwardedBeforeBuild)
}
