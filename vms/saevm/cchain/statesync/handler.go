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

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/network"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var _ adaptor.SyncableVM[*summary] = (*SummaryHandler)(nil)

// SummaryHandler wraps the SAE [statesync.SummaryHandler] with the C-Chain
// atomic trie state, so every served summary carries the atomic trie root at
// its height.
type SummaryHandler struct {
	*statesync.SummaryHandler

	network *network.Network
	log     logging.Logger

	hooks hook.Points
	state *state.State
	ethDB ethdb.Database

	// Lifecycle management, mirroring the embedded handler: err MUST only be
	// written before stateSyncDone is closed and only read after.
	mu            sync.Mutex
	stopped       bool
	cancel        context.CancelFunc
	err           error
	stateSyncDone chan struct{}
}

// New constructs a new [SummaryHandler] with the given configuration and
// database.
//
// TODO(alarso16): Add extra block verification
func New(
	cfg statesync.Config,
	db ethdb.Database,
	network *network.Network,
	hooks hook.Points,
	state *state.State,
	snowCtx *snow.Context,
) (*SummaryHandler, error) {
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
	return &SummaryHandler{
		SummaryHandler: inner,
		network:        network,
		log:            snowCtx.Log,
		state:          state,
		hooks:          hooks,
		ethDB:          db,
		stateSyncDone:  make(chan struct{}),
	}, nil
}

// Shutdown cancels any ongoing state sync (both the embedded handler's and
// the atomic trie's) and waits for the sync goroutine to exit, returning early
// with the context's error if ctx expires first. After Shutdown, no new sync
// can be started.
func (h *SummaryHandler) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.stopped = true
	cancel := h.cancel
	h.mu.Unlock()

	// The embedded handler is shut down first: cancelling its sync unblocks
	// our goroutine's wait on its result.
	if err := h.SummaryHandler.Shutdown(ctx); err != nil {
		return err
	}
	if cancel == nil {
		// no sync was ever started
		return nil
	}
	cancel()
	select {
	case <-h.stateSyncDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetStateSummary is the same as [statesync.SummaryHandler.GetStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetStateSummary(ctx context.Context, height uint64) (*summary, error) {
	return h.wrap(h.SummaryHandler.GetStateSummary(ctx, height))
}

// GetLastStateSummary is the same as [statesync.SummaryHandler.GetLastStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetLastStateSummary(ctx context.Context) (*summary, error) {
	return h.wrap(h.SummaryHandler.GetLastStateSummary(ctx))
}

// GetOngoingSyncStateSummary is the same as [statesync.SummaryHandler.GetOngoingSyncStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetOngoingSyncStateSummary(ctx context.Context) (*summary, error) {
	return h.wrap(h.SummaryHandler.GetOngoingSyncStateSummary(ctx))
}

// wrap pairs an SAE summary with the C-Chain atomic trie root at its height.
func (h *SummaryHandler) wrap(base *statesync.Summary, err error) (*summary, error) {
	if err != nil {
		return nil, err
	}

	settledHeight, err := h.settledHeight(base.BlockHash(), base.Height())
	if err != nil {
		return nil, err
	}

	root, err := h.state.GetRoot(settledHeight)
	if err != nil {
		return nil, err
	}
	return &summary{
		summary:     *base,
		settledRoot: root,
	}, nil
}

func (h *SummaryHandler) settledHeight(hash common.Hash, height uint64) (uint64, error) {
	hdr := rawdb.ReadHeader(h.ethDB, hash, height)
	if hdr == nil {
		return 0, database.ErrNotFound
	}
	return h.hooks.SettledBy(hdr).Height, nil
}
