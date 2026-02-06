// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/database/merkle/firewood/syncer"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/code"
	"github.com/ava-labs/avalanchego/graft/coreth/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	merklesync "github.com/ava-labs/avalanchego/database/merkle/sync"
)

var (
	_ types.Syncer    = (*FirewoodSyncer)(nil)
	_ types.Finalizer = (*FirewoodSyncer)(nil)
)

type FirewoodSyncer struct {
	s         *merklesync.Syncer[*syncer.RangeProof, struct{}]
	cancel    context.CancelFunc
	codeQueue *code.Queue
	// finalizeOnce is initialized in the constructor to make Finalize idempotent.
	finalizeOnce func() error
}

func NewFirewoodSyncer(config syncer.Config, db *ffi.Database, target common.Hash, codeQueue *code.Queue, rangeProofClient, changeProofClient *p2p.Client) (*FirewoodSyncer, error) {
	s, err := syncer.NewEVM(
		config,
		db,
		codeQueue,
		ids.ID(target),
		rangeProofClient,
		changeProofClient,
	)
	if err != nil {
		return nil, err
	}
	f := &FirewoodSyncer{
		s:         s,
		codeQueue: codeQueue,
	}
	f.finalizeOnce = sync.OnceValue(f.finish)
	return f, nil
}

func (f *FirewoodSyncer) Sync(ctx context.Context) error {
	ctx, f.cancel = context.WithCancel(ctx)
	if err := f.s.Sync(ctx); err != nil {
		return err
	}

	return f.Finalize()
}

func (f *FirewoodSyncer) Finalize() error {
	return f.finalizeOnce()
}

// finish performs the finalization logic for the FirewoodSyncer inside a [sync.Once].
// This is linked to the [sync.Once] in the constructor, and should not be called directly.
func (f *FirewoodSyncer) finish() error {
	// Ensure the syncer stops work and the code queue closes on exit.
	f.cancel()
	if err := f.codeQueue.Finalize(); err != nil {
		return fmt.Errorf("finalizing code queue: %w", err)
	}
	return nil
}

func (*FirewoodSyncer) ID() string {
	return "state_firewood_sync"
}

func (*FirewoodSyncer) Name() string {
	return "Firewood EVM State Syncer"
}
