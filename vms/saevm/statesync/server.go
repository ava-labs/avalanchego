// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

// RegisterServer registers the handlers for the state sync protocol, counting
// the requests they serve under the [Handler]'s metrics namespace.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *Handler) RegisterServer(tdb *triedb.Database, snap *snapshot.Tree) error {
	var (
		log    = h.snowCtx.Log
		p2pNet = h.network.Network
		db     = h.db
	)
	if err := block.RegisterHandler(log, p2pNet, db, h.reg); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	if err := hashdb.RegisterHandler(
		log,
		p2pNet,
		p2p.EVMLeafRequestHandlerID,
		tdb,
		common.HashLength,
		h.reg,
		hashdbOptions(snap)...,
	); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(log, p2pNet, db, h.reg); err != nil {
		return fmt.Errorf("registering code handler: %w", err)
	}

	return nil
}

func hashdbOptions(snap *snapshot.Tree) []hashdb.HandlerOption {
	if snap == nil {
		return nil
	}
	return []hashdb.HandlerOption{
		hashdb.WithSnapshot(&syncSnap{snap}),
	}
}

var _ hashdb.Snapshot = (*syncSnap)(nil)

// syncSnap adapts a [snapshot.Tree] to the interface needed for a
// [hashdb.Snapshot].
//
// TODO(alarso16): The iterators suffer from a race where the state can change
// during use due to execution calling [snapshot.Tree.Cap]. This doesn't
// violate correctness, but the iterators will error. An optimization can be
// made to iterate directly from disk, even if the state changes.
type syncSnap struct {
	snap *snapshot.Tree
}

func (s *syncSnap) AccountIterator(start common.Hash) (snapshot.AccountIterator, error) {
	return s.snap.AccountIterator(s.snap.DiskRoot(), start)
}

func (s *syncSnap) StorageIterator(account common.Hash, start common.Hash) (snapshot.StorageIterator, error) {
	return s.snap.StorageIterator(s.snap.DiskRoot(), account, start)
}
