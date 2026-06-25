// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package statesync wraps the functionality in [statesync] with the C-Chain
// specific state.
package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var _ adaptor.SyncableVM[*summary] = (*SummaryHandler)(nil)

// SummaryHandler wraps the SAE [statesync.SummaryHandler] with the C-Chain
// atomic trie state, so every served summary carries the atomic trie root at
// its height.
type SummaryHandler struct {
	*statesync.SummaryHandler

	hooks         hook.Points
	state         *state.State
	ethDB         ethdb.Database
	stateSyncDone chan struct{}
}

// New constructs a new [SummaryHandler] with the given configuration and
// database. The genesis block must be provided to allow the handler to return
// the summary of the genesis block.
func New(
	snowCtx *snow.Context,
	cfg statesync.Config,
	db ethdb.Database,
	hooks hook.Points,
	state *state.State,
	genesis *types.Block,
) (*SummaryHandler, error) {
	inner, err := statesync.New(
		cfg,
		snowCtx,
		db,
		genesis,
	)
	if err != nil {
		return nil, fmt.Errorf("creating SAE statesync handler: %v", err)
	}
	return &SummaryHandler{
		SummaryHandler: inner,
		state:          state,
		hooks:          hooks,
		ethDB:          db,
		stateSyncDone:  make(chan struct{}),
	}, nil
}

// Shutdown cancels any ongoing state sync.
func (h *SummaryHandler) Shutdown(ctx context.Context) error {
	if err := h.SummaryHandler.Shutdown(ctx); err != nil {
		return err
	}
	// TODO(alarso16): cancel any ongoing state sync
	return nil
}

// GetStateSummary is the same as [statesync.SummaryHandler.GetStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetStateSummary(ctx context.Context, height uint64) (*summary, error) {
	base, err := h.SummaryHandler.GetStateSummary(ctx, height)
	if err != nil {
		return nil, err
	}
	return h.wrap(base)
}

// GetLastStateSummary is the same as [statesync.SummaryHandler.GetLastStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetLastStateSummary(ctx context.Context) (*summary, error) {
	base, err := h.SummaryHandler.GetLastStateSummary(ctx)
	if err != nil {
		return nil, err
	}
	return h.wrap(base)
}

// GetOngoinSyncStateSummary is the same as [statesync.SummaryHandler.GetOngoingSyncStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetOngoingSyncStateSummary(ctx context.Context) (*summary, error) {
	base, err := h.SummaryHandler.GetOngoingSyncStateSummary(ctx)
	if err != nil {
		return nil, err
	}
	return h.wrap(base)
}

// wrap pairs an SAE summary with the C-Chain atomic trie root at its height.
func (h *SummaryHandler) wrap(base *statesync.Summary) (*summary, error) {
	// Genesis block may not be on disk.
	if base.Height() == 0 {
		return h.wrapAtHeight(base, 0)
	}

	hdr := rawdb.ReadHeader(h.ethDB, base.BlockHash(), base.Height())
	if hdr == nil {
		return nil, fmt.Errorf("can't find header for block %s at height %d", base.BlockHash(), base.Height())
	}
	settledHeight := h.hooks.SettledBy(hdr).Height
	return h.wrapAtHeight(base, settledHeight)
}

func (h *SummaryHandler) wrapAtHeight(base *statesync.Summary, settledHeight uint64) (*summary, error) {
	root, err := h.state.GetRoot(settledHeight)
	if err != nil {
		return nil, err
	}
	return &summary{
		summary:     *base,
		settledRoot: root,
	}, nil
}
