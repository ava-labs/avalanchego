// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package statesync wraps the functionality in [statesync] with the C-Chain
// specific state.
package statesync

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var _ adaptor.SyncableVM[*summary] = (*Handler)(nil)

// Handler wraps the SAE [statesync.Handler] with the cross-chain state at the
// settled height. It provides a full implementation of [adaptor.SyncableVM] to
// be used with a VM to provide state sync functionality.
//
// TODO(StephenButtolph): Investigate better abstracting syncing in the handler.
type Handler struct {
	*statesync.Handler

	cfg     statesync.Config
	hooks   hook.Points
	state   *state.State
	network *network.Network
	ethDB   ethdb.Database
	snowCtx *snow.Context

	// Lifecycle management
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	err     utils.Atomic[error]
	done    chan struct{}
}

// New constructs a new [Handler] with the given configuration and
// database.
func New(
	cfg statesync.Config,
	db ethdb.Database,
	snowCtx *snow.Context,
	network *network.Network,
	hooks hook.Points,
	state *state.State,
) (*Handler, error) {
	inner, err := statesync.New(
		cfg,
		db,
		snowCtx,
		network,
		hooks,
	)
	if err != nil {
		return nil, fmt.Errorf("creating SAE statesync handler: %v", err)
	}
	return &Handler{
		Handler: inner,
		cfg:     cfg,
		state:   state,
		hooks:   hooks,
		network: network,
		ethDB:   db,
		snowCtx: snowCtx,
		done:    make(chan struct{}),
	}, nil
}

// Shutdown cancels any ongoing state sync and waits for the sync goroutine to
// exit, returning early with the context's error if ctx expires first. After
// Shutdown, no new sync can be started.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.stopped = true
	cancel := h.cancel
	h.mu.Unlock()

	if cancel == nil {
		// no sync was ever started
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

// GetStateSummary is the same as [statesync.Handler.GetStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *Handler) GetStateSummary(ctx context.Context, height uint64) (*summary, error) {
	return h.wrap(h.Handler.GetStateSummary(ctx, height))
}

// GetLastStateSummary is the same as [statesync.Handler.GetLastStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *Handler) GetLastStateSummary(ctx context.Context) (*summary, error) {
	return h.wrap(h.Handler.GetLastStateSummary(ctx))
}

// GetOngoingSyncStateSummary is not implemented. It always returns
// [database.ErrNotFound].
//
// TODO(alarso16): Allow resuming state sync.
func (*Handler) GetOngoingSyncStateSummary(ctx context.Context) (*summary, error) {
	return nil, database.ErrNotFound
}

// wrap pairs an SAE summary with the C-Chain atomic trie root at the
// corresponding block's settled height.
func (h *Handler) wrap(base *statesync.Summary, err error) (*summary, error) {
	if err != nil {
		return nil, err
	}

	settledHeight, err := h.settledHeight(base.AcceptedHash, base.AcceptedHeight)
	if err != nil {
		h.snowCtx.Log.Error("getting settled height for summary",
			zap.Uint64("acceptedHeight", base.AcceptedHeight),
			zap.Stringer("acceptedHash", base.AcceptedHash),
			zap.Error(err),
		)
		return nil, err
	}

	root, err := h.state.GetRoot(settledHeight)
	if err != nil {
		h.snowCtx.Log.Error("getting settled cross-chain state root",
			zap.Uint64("acceptedHeight", base.AcceptedHeight),
			zap.Stringer("acceptedHash", base.AcceptedHash),
			zap.Uint64("settledHeight", settledHeight),
			zap.Error(err),
		)
		return nil, err
	}
	return &summary{
		summary:     *base,
		settledRoot: root,
	}, nil
}

func (h *Handler) settledHeight(hash common.Hash, height uint64) (uint64, error) {
	hdr := rawdb.ReadHeader(h.ethDB, hash, height)
	if hdr == nil {
		return 0, database.ErrNotFound
	}
	return h.hooks.SettledBy(hdr).Height, nil
}
