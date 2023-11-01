// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrStateSyncableVMNotImplemented = errors.New("vm does not implement StateSyncableVM interface")
	ErrBlockBackfillingNotEnabled    = errors.New("vm does not require block backfilling")
	ErrStopBlockBackfilling          = errors.New("vm required stopping block backfilling")
)

// StateSyncableVM contains the functionality to allow VMs to sync to a given
// state, rather then boostrapping from genesis.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM.
	// If StateSyncableVM is not implemented, as it may happen with a wrapper
	// VM, StateSyncEnabled should return false, nil
	StateSyncEnabled(context.Context) (bool, error)

	// GetOngoingSyncStateSummary returns an in-progress state summary if it
	// exists.
	//
	// The engine can then ask the network if the ongoing summary is still
	// supported, thus helping the VM decide whether to continue an in-progress
	// sync or start over.
	//
	// Returns database.ErrNotFound if there is no in-progress sync.
	GetOngoingSyncStateSummary(context.Context) (StateSummary, error)

	// GetLastStateSummary returns the latest state summary.
	//
	// Returns database.ErrNotFound if no summary is available.
	GetLastStateSummary(context.Context) (StateSummary, error)

	// ParseStateSummary parses a state summary out of [summaryBytes].
	ParseStateSummary(ctx context.Context, summaryBytes []byte) (StateSummary, error)

	// GetStateSummary retrieves the state summary that was generated at height
	// [summaryHeight].
	//
	// Returns database.ErrNotFound if no summary is available at
	// [summaryHeight].
	GetStateSummary(ctx context.Context, summaryHeight uint64) (StateSummary, error)

	// BackfillBlocksEnabled checks if VM wants to download all blocks from state summary one
	// down to genesis.
	//
	// Returns the ID and height of the block it wants to start backfilling from.
	// Returns ErrBlockBackfillingNotEnabled if block backfilling is not enabled.
	// BackfillBlocksEnabled can be called multiple times by the engine
	BackfillBlocksEnabled(ctx context.Context) (ids.ID, uint64, error)

	// BackfillBlocks passes blocks bytes retrieved via GetAncestors calls to the VM
	// It's left to the VM the to parse, validate and index the blocks.
	// BackfillBlocks returns the next block ID to be requested and an error
	// Returns the ID and height of the block it wants to start backfilling from.
	// Returns [ErrStopBlockBackfilling] if VM has done backfilling; engine will stop requesting blocks.
	// If BackfillBlocks returns any other error, engine will issue a GetAncestor call to a different peer
	// with the previously requested block ID
	BackfillBlocks(ctx context.Context, blocks [][]byte) (ids.ID, uint64, error)
}
