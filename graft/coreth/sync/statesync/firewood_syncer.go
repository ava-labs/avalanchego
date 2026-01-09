// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/firewood/syncer"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"

	syncpkg "github.com/ava-labs/avalanchego/graft/coreth/sync"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var _ syncpkg.Syncer = (*firewoodSyncer)(nil)

type firewoodSyncer struct {
	s         *xsync.Syncer[*syncer.RangeProof, struct{}]
	codeQueue *CodeQueue
}

func NewFirewoodSyncer(config syncer.Config, db *ffi.Database, target common.Hash, codeQueue *CodeQueue, rangeProofClient, changeProofClient *p2p.Client) (syncpkg.Syncer, error) {
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
	return &firewoodSyncer{
		s:         s,
		codeQueue: codeQueue,
	}, nil
}

func (f *firewoodSyncer) Sync(ctx context.Context) error {
	if err := f.s.Start(ctx); err != nil {
		return fmt.Errorf("starting syncer: %w", err)
	}

	if err := f.s.Wait(ctx); err != nil {
		return fmt.Errorf("waiting for syncer: %w", err)
	}
	return f.codeQueue.Finalize()
}

func (*firewoodSyncer) ID() string {
	return "firewood_syncer"
}

func (*firewoodSyncer) Name() string {
	return "Firewood Syncer"
}
