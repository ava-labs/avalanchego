// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var ErrStateSyncableVMNotImplemented = errors.New("vm does not implement StateSyncableVM interface")

// Summary represents the information needed for state sync processing
type Summary struct {
	Key     []byte // Should uniquely identify Summary
	Content []byte // actual state summary content
}

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// Register nodes from which fast sync summaries can be downloaded
	RegisterFastSyncer(fastSyncer []ids.ShortID) error

	// Enabled indicates whether the state sync is enabled for this VM
	StateSyncEnabled() (bool, error)
	// StateSummary returns latest Summary with an optional error
	StateSyncGetLastSummary() (Summary, error)
	// IsAccepted returns true if input []bytes represent a valid state summary
	// for state sync.
	StateSyncIsSummaryAccepted(key []byte) (bool, error)

	// SyncState is called with a list of valid summaries to sync from.
	// These summaries were collected from peers and validated with validators.
	// VM will use information inside the summary to choose one and sync
	// its state to that summary. Normal bootstrapping resumes after this
	// function returns.
	// Will be called with [nil] if no valid state summaries could be found.
	StateSync([]Summary) error
}
