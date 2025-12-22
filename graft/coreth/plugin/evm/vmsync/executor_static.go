// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmsync

import (
	"context"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
)

var _ Executor = (*staticExecutor)(nil)

// staticExecutor runs syncers sequentially without block queueing.
// This is the default sync mode where all syncers complete before
// committing results, with no concurrent block processing.
type staticExecutor struct {
	registry *SyncerRegistry
	acceptor Acceptor
}

func newStaticExecutor(registry *SyncerRegistry, acceptor Acceptor) *staticExecutor {
	return &staticExecutor{
		registry: registry,
		acceptor: acceptor,
	}
}

// Execute runs the sync process and blocks until completion or error.
// For static sync, this runs all syncers and then accepts the synced state into the VM.
func (e *staticExecutor) Execute(ctx context.Context, summary message.Syncable) error {
	if err := e.registry.RunSyncerTasks(ctx, summary); err != nil {
		return err
	}
	return e.acceptor.AcceptSync(ctx, summary)
}

// OnBlockAccepted is a no-op for static sync since blocks are not queued.
func (*staticExecutor) OnBlockAccepted(EthBlockWrapper) (bool, error) {
	return false, nil
}

// OnBlockRejected is a no-op for static sync since blocks are not queued.
func (*staticExecutor) OnBlockRejected(EthBlockWrapper) (bool, error) {
	return false, nil
}

// OnBlockVerified is a no-op for static sync since blocks are not queued.
func (*staticExecutor) OnBlockVerified(EthBlockWrapper) (bool, error) {
	return false, nil
}
