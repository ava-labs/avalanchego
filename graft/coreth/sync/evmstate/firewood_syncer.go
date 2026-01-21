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
	"github.com/ava-labs/avalanchego/graft/coreth/sync/code"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync/types"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ syncpkg.Syncer   = (*FirewoodSyncer)(nil)
	_ syncpkg.Finalizer = (*FirewoodSyncer)(nil)
)

type FirewoodSyncer struct {
	s         *xsync.Syncer[*syncer.RangeProof, struct{}]
	codeQueue *code.Queue
	// finalizeOnce is initialized in the constructor to make Finalize idempotent.
	finalizeOnce func() error
}

func NewFirewoodSyncer(config syncer.Config, db *ffi.Database, target common.Hash, codeQueue *code.Queue, rangeProofClient, changeProofClient *p2p.Client) (syncpkg.Syncer, error) {
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
	fw := &FirewoodSyncer{
		s:         s,
		codeQueue: codeQueue,
	}
	fw.finalizeOnce = sync.OnceValue(func() error {
		// Ensure the syncer stops work and the code queue closes on exit.
		fw.s.Close()
		if err := fw.codeQueue.Finalize(); err != nil {
			return fmt.Errorf("finalizing code queue: %w", err)
		}
		return nil
	})
	return fw, nil
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

func (*FirewoodSyncer) ID() string {
	return "state_firewood_sync"
}

func (*FirewoodSyncer) Name() string {
	return "Firewood EVM State Syncer"
}

func (f *FirewoodSyncer) Finalize() error {
	return f.finalizeOnce()
}
