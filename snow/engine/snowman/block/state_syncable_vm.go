// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
)

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

	// StateSummaryDynamic indicates that engine should immediately transition
	// into bootstrapping and then normal consensus. State sync will proceed
	// asynchronously in the VM.
	StateSummaryDynamic
)

var ErrStateSyncableVMNotImplemented = errors.New("vm does not implement StateSyncableVM interface")

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
}
