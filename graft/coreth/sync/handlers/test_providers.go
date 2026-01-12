// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handlers

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
)

var (
	_ BlockProvider    = (*TestBlockProvider)(nil)
	_ SnapshotProvider = (*TestSnapshotProvider)(nil)
)

type TestBlockProvider struct {
	GetBlockFn func(common.Hash, uint64) *types.Block
}

func (t *TestBlockProvider) GetBlock(hash common.Hash, number uint64) *types.Block {
	return t.GetBlockFn(hash, number)
}

type TestSnapshotProvider struct {
	Snapshot *snapshot.Tree
}

func (t *TestSnapshotProvider) Snapshots() *snapshot.Tree {
	return t.Snapshot
}
