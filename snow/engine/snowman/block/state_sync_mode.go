// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

// StateSyncMode is returned by the StateSyncableVM when a state summary is
// passed to it. It indicates which type of state sync the VM is performing.
type StateSyncMode uint8

const (
	// StateSyncSkipped indicates that state sync won't be run by the VM. This
	// may happen if the VM decides that the state sync is too recent and it
	// would be faster to bootstrap the missing blocks.
	StateSyncSkipped StateSyncMode = iota + 1

	// StateSyncStatic indicates that engine should stop and wait for the VM to
	// complete state syncing before moving ahead with bootstrapping.
	StateSyncStatic

	// StateSyncDynamic indicates that engine should immediately transition
	// into bootstrapping and then normal consensus. State sync will proceed
	// asynchronously in the VM.
	//
	// Invariant: If this is returned it is assumed that the VM should be able
	// to handle requests from the engine as if the VM is fully synced.
	// Specifically, it is required that the invariants specified by
	// LastAccepted, GetBlock, ParseBlock, and Block.Verify are maintained. This
	// means that when StateSummary.Accept returns, the block that would become
	// the last accepted block must be immediately fetchable by the engine.
	StateSyncDynamic
)

func (s StateSyncMode) String() string {
	switch s {
	case StateSyncSkipped:
		return "Skipped"
	case StateSyncStatic:
		return "Static"
	case StateSyncDynamic:
		return "Dynamic"
	default:
		return "Unknown"
	}
}
