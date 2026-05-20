// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"sync"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/database/merkle/firewood/syncer"
	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	merklesync "github.com/ava-labs/avalanchego/database/merkle/sync"
)

var (
	_ types.Syncer    = (*FirewoodSyncer)(nil)
	_ types.Finalizer = (*FirewoodSyncer)(nil)
)

type FirewoodSyncer struct {
	s      *merklesync.Syncer[*syncer.RangeProof, struct{}]
	cancel context.CancelFunc

	// finalizeCodeQueue guards the single call to codeQueue.Finalize().
	// Both Sync() (on success) and Finalize() (best-effort cleanup) go through
	// this to avoid double-finalize errors since Queue.Finalize is not idempotent.
	finalizeCodeQueue func() error
}

func NewFirewoodSyncer(config syncer.Config, db *ffi.Database, target common.Hash, codeQueue types.CodeRequestQueue, rpClient, cpClient *p2p.Client) (*FirewoodSyncer, error) {
	s, err := syncer.NewEVM(
		config,
		db,
		codeQueue,
		ids.ID(target),
		rpClient,
		cpClient,
	)
	if err != nil {
		return nil, err
	}
	f := &FirewoodSyncer{
		s:                 s,
		cancel:            func() {}, // overwritten in Sync
		finalizeCodeQueue: sync.OnceValue(codeQueue.Finalize),
	}
	return f, nil
}

// Sync runs the firewood state syncer to completion or until the context is
// cancelled. On successful completion it finalizes the code queue so the code
// syncer can exit.
func (f *FirewoodSyncer) Sync(ctx context.Context) error {
	ctx, f.cancel = context.WithCancel(ctx)
	if err := f.s.Sync(ctx); err != nil {
		return err
	}
	return f.finalizeCodeQueue()
}

// Finalize performs best-effort cleanup: cancels the sync context and finalizes
// the code queue. It is idempotent and safe to call multiple times.
func (f *FirewoodSyncer) Finalize() error {
	f.cancel()
	return f.finalizeCodeQueue()
}

func (*FirewoodSyncer) ID() string {
	return "state_firewood_sync"
}

func (*FirewoodSyncer) Name() string {
	return "Firewood EVM State Syncer"
}

// UpdateTarget forwards the new target root to the underlying merkle syncer,
// which re-prioritizes completed work items for re-sync against the new root.
// It is thread-safe, non-blocking, and safe to call while Sync is running.
func (f *FirewoodSyncer) UpdateTarget(target message.Syncable) error {
	return f.s.UpdateSyncTarget(ids.ID(target.GetBlockRoot()))
}
