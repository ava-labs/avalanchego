// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/state"
	"github.com/ava-labs/avalanchego/vms/evm/statesync"
	"github.com/ava-labs/avalanchego/vms/evm/statesync/acceptor"
)

var _ acceptor.Syncer[*summary] = (*syncer)(nil)

// Acceptor asynchronously syncs to an accepted [summary], including the
// cross-chain state.
type Acceptor = acceptor.AsyncAcceptor[*summary]

// NewAcceptor returns the [Acceptor] that syncs the databases of the
// [Handler]. Its Shutdown method MUST be called to release resources.
func (h *Handler) NewAcceptor() *Acceptor {
	return acceptor.New(h.cfg.Enabled, func() (acceptor.Syncer[*summary], error) {
		return h.newSyncer(), nil
	})
}

// syncer wraps the SAE [statesync.Syncer] to additionally sync the cross-chain
// state at the settled height.
type syncer struct {
	evm *statesync.Syncer
	h   *Handler
}

// newSyncer returns the [acceptor.Syncer] for a single state sync.
func (h *Handler) newSyncer() acceptor.Syncer[*summary] {
	return &syncer{
		evm: statesync.NewSyncer(
			h.cfg,
			h.hooks,
			h.snowCtx,
			h.network,
			h.ethDB,
		),
		h: h,
	}
}

func (s *syncer) ShouldAcceptSummary(sum *summary) bool {
	return s.evm.ShouldAcceptSummary(&sum.summary)
}

// Sync performs the EVM sync, followed by the cross-chain state sync.
func (s *syncer) Sync(ctx context.Context, sum *summary) error {
	if err := s.evm.Sync(ctx, &sum.summary); err != nil {
		return err
	}

	// We can only determine the settled height after we have fetched the last
	// accepted block, which is fetched during the EVM sync.
	h := s.h
	settledHeight, err := h.settledHeight(sum.summary.AcceptedHash, sum.summary.AcceptedHeight)
	if err != nil {
		return err
	}

	h.snowCtx.Log.Info("syncing cross-chain state",
		zap.Stringer("settledCrossChainRoot", sum.settledRoot),
		zap.Uint64("settledHeight", settledHeight),
		zap.Stringer("acceptedHash", sum.summary.AcceptedHash),
		zap.Uint64("acceptedHeight", sum.summary.AcceptedHeight),
	)
	crossChainClient, err := h.network.SyncClient(p2p.EVMAtomicLeafRequestHandlerID)
	if err != nil {
		return err
	}
	crossChainSyncer := state.NewSyncer(
		crossChainClient,
		h.state,
		sum.settledRoot,
		settledHeight,
	)
	if err := crossChainSyncer.Sync(ctx); err != nil {
		return err
	}
	h.snowCtx.Log.Info("finished syncing cross-chain state")
	return nil
}

func (s *syncer) WriteSynced(sum *summary) error {
	return s.evm.WriteSynced(&sum.summary)
}
