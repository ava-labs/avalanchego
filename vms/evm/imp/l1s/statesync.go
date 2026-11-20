// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"sync"

	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/network"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

var _ adaptor.SyncableVM[*statesync.Summary] = (*stateSyncHandler)(nil)

// stateSyncHandler is a [statesync.Handler] that can also perform a state
// sync. An L1 shares no state with its peers beyond the EVM state, so
// syncing the EVM is sufficient.
type stateSyncHandler struct {
	*statesync.Handler

	cfg     statesync.Config
	hooks   hook.Points
	network *network.Network
	ethDB   ethdb.Database
	snowCtx *snow.Context

	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	err     utils.Atomic[error]
	done    chan struct{} // closed once a started sync has finished
}

func newStateSyncHandler(
	cfg statesync.Config,
	ethDB ethdb.Database,
	snowCtx *snow.Context,
	network *network.Network,
	hooks hook.Points,
) (*stateSyncHandler, error) {
	inner, err := statesync.New(cfg, ethDB, snowCtx, network, hooks)
	if err != nil {
		return nil, err
	}
	return &stateSyncHandler{
		Handler: inner,
		cfg:     cfg,
		hooks:   hooks,
		network: network,
		ethDB:   ethDB,
		snowCtx: snowCtx,
		done:    make(chan struct{}),
	}, nil
}

// HealthCheck always reports healthy.
func (*stateSyncHandler) HealthCheck(context.Context) (any, error) {
	return nil, nil
}

// StateSyncEnabled returns [statesync.Config.Enabled].
func (h *stateSyncHandler) StateSyncEnabled(context.Context) (bool, error) {
	return h.cfg.Enabled, nil
}

// GetOngoingSyncStateSummary always returns [database.ErrNotFound], as an
// interrupted sync restarts from scratch.
func (*stateSyncHandler) GetOngoingSyncStateSummary(context.Context) (*statesync.Summary, error) {
	return nil, database.ErrNotFound
}

// AcceptSummary returns [block.StateSyncSkipped] if this node shouldn't sync
// to s. Otherwise, it starts syncing to s in the background and returns
// [block.StateSyncStatic]. Once the sync finishes, [stateSyncHandler.WaitForEvent]
// returns [snowcommon.StateSyncDone] and [stateSyncHandler.SyncError] returns
// its result.
//
// AcceptSummary MUST be called at most once.
func (h *stateSyncHandler) AcceptSummary(ctx context.Context, s *statesync.Summary) (block.StateSyncMode, error) {
	syncer := statesync.NewSyncer(h.cfg, h.hooks, h.snowCtx, h.network, h.ethDB)
	if !syncer.ShouldAcceptSummary(s) {
		return block.StateSyncSkipped, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return block.StateSyncSkipped, nil
	}

	// The sync outlives this call, so it MUST NOT be cancelled with ctx.
	ctx, h.cancel = context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer h.cancel()
		defer close(h.done) // MUST happen after h.err is set

		if err := syncer.Sync(ctx, s); err != nil {
			h.err.Set(err)
			return
		}
		h.err.Set(syncer.WriteSynced(s))
	}()
	return block.StateSyncStatic, nil
}

// WaitForEvent returns [snowcommon.StateSyncDone] once a sync started by
// [stateSyncHandler.AcceptSummary] has finished.
func (h *stateSyncHandler) WaitForEvent(ctx context.Context) (snowcommon.Message, error) {
	select {
	case <-h.done:
		return snowcommon.StateSyncDone, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// SyncError returns the error of a finished sync, or nil if the sync succeeded
// or hasn't finished.
func (h *stateSyncHandler) SyncError() error {
	return h.err.Get()
}

// Shutdown cancels any ongoing sync and waits for it to exit, returning early
// with ctx's error if ctx is cancelled first. Afterwards,
// [stateSyncHandler.AcceptSummary] never starts a sync.
func (h *stateSyncHandler) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.stopped = true
	cancel := h.cancel
	h.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
