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

// SummaryHandler wraps the SAE [statesync.Handler] with the C-Chain
// atomic trie state at the settled height.
type SummaryHandler struct {
	*statesync.Handler

	cfg   statesync.Config
	hooks hook.Points
	state *state.State
	ethDB ethdb.Database
	log   logging.Logger
}

// New constructs a new [SummaryHandler] with the given configuration and
// database.
func New(
	cfg statesync.Config,
	db ethdb.Database,
	snowCtx *snow.Context,
	network *network.Network,
	hooks hook.Points,
	state *state.State,
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
		Handler: inner,
		cfg:     cfg,
		state:   state,
		hooks:   hooks,
		ethDB:   db,
		log:     snowCtx.Log,
	}, nil
}

// GetStateSummary is the same as [statesync.Handler.GetStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetStateSummary(ctx context.Context, height uint64) (*summary, error) {
	return h.wrap(h.Handler.GetStateSummary(ctx, height))
}

// GetLastStateSummary is the same as [statesync.Handler.GetLastStateSummary],
// but the returned summary contains the settled C-Chain state root.
func (h *SummaryHandler) GetLastStateSummary(ctx context.Context) (*summary, error) {
	return h.wrap(h.Handler.GetLastStateSummary(ctx))
}

// GetOngoingSyncStateSummary is not implemented. It always returns
// [database.ErrNotFound].
//
// TODO(alarso16): Allow resuming state sync.
func (*SummaryHandler) GetOngoingSyncStateSummary(ctx context.Context) (*summary, error) {
	return nil, database.ErrNotFound
}

// wrap pairs an SAE summary with the C-Chain atomic trie root at the
// corresponding block's settled height.
//
// Any database errors are logged at [logging.Error] and returned to the caller.
func (h *SummaryHandler) wrap(base *statesync.Summary, err error) (*summary, error) {
	if err != nil {
		return nil, err
	}

	hdr := rawdb.ReadHeader(h.ethDB, base.AcceptedHash, base.AcceptedHeight)
	if hdr == nil {
		h.log.Error("missing header",
			zap.Uint64("acceptedHeight", base.AcceptedHeight),
			zap.Stringer("acceptedHash", base.AcceptedHash),
		)
		return nil, fmt.Errorf("missing header %d with hash %s", base.AcceptedHeight, base.AcceptedHash)
	}
	settledHeight := h.hooks.SettledBy(hdr).Height
	root, err := h.state.GetRoot(settledHeight)
	if err != nil {
		h.log.Error("getting settled cross-chain state root",
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
