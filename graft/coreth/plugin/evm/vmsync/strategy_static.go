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
// committing results, with no concurrent block processing.
type staticStrategy struct {
	registry  *SyncerRegistry
	committer Committer
}

func newStaticStrategy(registry *SyncerRegistry, committer Committer) *staticStrategy {
	return &staticStrategy{
		registry:  registry,
		committer: committer,
	}
}

// Start begins the sync process and blocks until completion or error.
// For static sync, this runs all syncers and then commits the results to the VM.
func (s *staticStrategy) Start(ctx context.Context, summary message.Syncable) error {
	if err := s.registry.RunSyncerTasks(ctx, summary); err != nil {
		return err
	}
	return s.committer.Commit(ctx, summary)
}
