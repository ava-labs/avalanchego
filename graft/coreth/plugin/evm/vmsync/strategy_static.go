// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
)

var _ SyncStrategy = (*staticStrategy)(nil)

// staticStrategy runs syncers sequentially without block queueing.
// This is the default sync mode where all syncers complete before
// finalization, with no concurrent block processing.
type staticStrategy struct {
	registry  *SyncerRegistry
	finalizer *finalizer
	summary   message.Syncable
}

func newStaticStrategy(registry *SyncerRegistry, finalizer *finalizer, summary message.Syncable) *staticStrategy {
	return &staticStrategy{
		registry:  registry,
		finalizer: finalizer,
		summary:   summary,
	}
}

// Start begins the sync process and blocks until completion or error.
// For static sync, this runs all syncers and then finalizes the VM state.
func (s *staticStrategy) Start(ctx context.Context) error {
	if err := s.registry.RunSyncerTasks(ctx, s.summary); err != nil {
		return err
	}
	return s.finalizer.finalize(ctx, s.summary)
}
