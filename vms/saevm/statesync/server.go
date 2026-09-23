// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

// RegisterHandlers registers the handlers for the state sync protocol.
//
// TODO(alarso16): Find a way to wire through Firewood.
func RegisterHandlers(
	log logging.Logger,
	network *p2p.Network,
	db ethdb.Database,
	tdb *triedb.Database,
	snap *snapshot.Tree,
) error {
	if err := block.RegisterHandler(log, network, db); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	if err := hashdb.RegisterHandler(
		log,
		network,
		p2p.EVMLeafRequestHandlerID,
		tdb,
		common.HashLength,
		hashdbOptions(snap)...,
	); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(log, network, db); err != nil {
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
