// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package statesync wraps the functionality in [statesync] with the C-Chain
// specific state.
package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var _ adaptor.SyncableVM[*summary] = (*SummaryHandler)(nil)

// SummaryHandler wraps the SAE [statesync.SummaryHandler] with the C-Chain
// atomic trie state at the settled height.
type SummaryHandler struct {
	*statesync.SummaryHandler

	hooks hook.Points
	state *state.State
	ethDB ethdb.Database
	log   logging.Logger

	stateSyncDone chan struct{}
}

// New constructs a new [SummaryHandler] with the given configuration and
// database.
func New(
	cfg statesync.Config,
	db ethdb.Database,
	hooks hook.Points,
	state *state.State,
	log logging.Logger,
) (*SummaryHandler, error) {
	inner, err := statesync.New(
		cfg,
		db,
		log,
		hooks,
	)
	if err != nil {
		return nil, fmt.Errorf("creating SAE statesync handler: %v", err)
	}
	return &SummaryHandler{
		SummaryHandler: inner,
		state:          state,
		hooks:          hooks,
		ethDB:          db,
		log:            log,
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

// wrap pairs an SAE summary with the C-Chain atomic trie root at the
// corresponding block's settled height.
//
// Any database errors are logged at [logging.Error] and returned to the caller.
func (h *SummaryHandler) wrap(base *statesync.Summary, err error) (*summary, error) {
	if err != nil {
		return nil, err
	}

	hdr := rawdb.ReadHeader(h.ethDB, base.BlockHash(), base.Height())
	if hdr == nil {
		h.log.Error("can't find header for block in database", zap.Stringer("blockHash", base.BlockHash()), zap.Uint64("height", base.Height()))
		return nil, fmt.Errorf("can't find header for block %s at height %d", base.BlockHash(), base.Height())
	}
	settledHeight := h.hooks.SettledBy(hdr).Height

	root, err := h.state.GetRoot(settledHeight)
	if err != nil {
		h.log.Error("getting settled C-Chain state root for state summary", zap.Uint64("settledHeight", settledHeight), zap.Error(err))
		return nil, err
	}
	return &summary{
		summary:     *base,
		settledRoot: root,
	}, nil
}
