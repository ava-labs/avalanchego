// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
)

var ErrStateSyncableVMNotImplemented = errors.New("vm does not implement StateSyncableVM interface")

// StateSyncableVM represents functionalities to allow Snowman VMs to sync to a given state,
// rather then boostrapping from genesis.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM.
	// If StateSyncableVM is not really implemented (as it may happen with rpcchainvm)
	// StateSyncEnabled should return false, nil
	StateSyncEnabled() (bool, error)

	// GetOngoingSyncStateSummary returns an in-progress state summary if it exists.
	// The engine can then ask the network if the ongoing summary is still supported,
	// thus helping StateSyncableVM to decide whether to continue an in-progress sync
	// or start over.
	// Returns database.ErrNotFound if there is no in-progress sync.
	GetOngoingSyncStateSummary() (Summary, error)

	// GetLastStateSummary returns latest Summary with an optional error
	// Returns database.ErrNotFound if summary is not available
	GetLastStateSummary() (Summary, error)

	// ParseStateSummary builds a Summary out of summaryBytes
	ParseStateSummary(summaryBytes []byte) (Summary, error)

	// GetStateSummary retrieves the summary related to height, if available.
	// Returns database.ErrNotFound if summary is not available
	GetStateSummary(summaryHeight uint64) (Summary, error)
}
