// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
)

type BlockProvider interface {
	GetBlock(common.Hash, uint64) *types.Block
}

// SnapshotTree abstracts the snapshot.Tree functionality needed for sync handlers.
// This allows different VM implementations to provide their own snapshot trees.
type SnapshotTree interface {
	// DiskAccountIterator returns an iterator for disk-based account snapshots.
	DiskAccountIterator(seek common.Hash) snapshot.AccountIterator
	// DiskStorageIterator returns an iterator for disk-based storage snapshots.
	DiskStorageIterator(account common.Hash, seek common.Hash) snapshot.StorageIterator
}

type SnapshotProvider interface {
	Snapshots() SnapshotTree
}

type SyncDataProvider interface {
	BlockProvider
	SnapshotProvider
}
