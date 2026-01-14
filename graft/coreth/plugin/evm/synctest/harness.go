// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package synctest provides test harness for running state sync tests with
// coreth-specific dependencies. This package bridges the generic test infrastructure
// in graft/evm/sync/synctest with coreth's snapshot implementation.
package synctest

import (
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/graft/coreth/core/state/snapshot"
	"github.com/ava-labs/avalanchego/graft/evm/sync/evmstate"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
)

// NewCorethTestHarness creates a TestHarness configured with coreth's snapshot implementation.
// This harness can be used to run the standard state sync tests from graft/evm/sync/synctest.
func NewCorethTestHarness() *synctest.TestHarness {
	return &synctest.TestHarness{
		SnapshotFactory: NewCorethSnapshotFactory(),
		WipeSnapshot:    WipeSnapshot,
	}
}

// NewCorethSnapshotFactory returns a SnapshotFactory that creates coreth snapshot iterables.
func NewCorethSnapshotFactory() evmstate.SnapshotFactory {
	return func(db ethdb.Database) evmstate.SnapshotIterable {
		return snapshot.NewDiskLayer(db)
	}
}

// WipeSnapshot wipes the coreth snapshot database.
func WipeSnapshot(db ethdb.Database, root bool) <-chan struct{} {
	return snapshot.WipeSnapshot(db, root)
}
