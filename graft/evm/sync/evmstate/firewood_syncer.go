// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/firewood/syncer"
	"github.com/ava-labs/avalanchego/graft/evm/sync/code"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ types.Syncer    = (*FirewoodSyncer)(nil)
	_ types.Finalizer = (*FirewoodSyncer)(nil)
)

type FirewoodSyncer struct {
	s         *xsync.Syncer[*syncer.RangeProof, struct{}]
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
	if err := f.s.Start(ctx); err != nil {
		return fmt.Errorf("starting syncer: %w", err)
	}

	if err := f.s.Wait(ctx); err != nil {
		return fmt.Errorf("waiting for syncer: %w", err)
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
	f.s.Close()
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
