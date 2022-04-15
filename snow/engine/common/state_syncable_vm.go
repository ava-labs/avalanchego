// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrStateSyncableVMNotImplemented = errors.New("vm does not implement StateSyncableVM interface")
	ErrUnknownStateSummary           = errors.New("state summary not found")
	ErrNoStateSyncOngoing            = errors.New("no state sync ongoing")
)

// Summary represents all the information needed for state sync processing.
// Summary must allow a VM to download, verify and rebuild its state,
// no matter whether it is freshly created or it has previous state.
// Both Key and ID uniquely identify a Summary. However:
// Key is used to efficiently elicit network votes;
// ID must allow Summary verification and it is returned into elicited votes;
// to allow state syncing nodes verify vote correctness.
// Finally Byte returns the Summary content which is defined by the VM and opaque to the engine.
type Summary interface {
	Bytes() []byte
	Key() uint64
	ID() ids.ID
}

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM.
	// If StateSyncableVM is not really implemented (as it may happen with rpcchainvm)
	// StateSyncEnabled should return false, nil
	StateSyncEnabled() (bool, error)

	// StateSyncGetOngoingSummary returns an in-progress state summary if it exists. This
	// allows the engine to ask the network if the ongoing summary is still supported by the
	// network. This simplifies the task of the StateSyncableVM to decide whether to
	// continue an in-progress sync or start over.
	// Returns ErrNoStateSyncOngoing if no local state summary exists.
	StateSyncGetOngoingSummary() (Summary, error)

	// StateSyncGetLastSummary returns latest Summary with an optional error
	// Returns ErrUnknownStateSummary if summary is not available
	StateSyncGetLastSummary() (Summary, error)

	// StateSyncParseSummary builds a Summary out of summaryBytes
	StateSyncParseSummary(summaryBytes []byte) (Summary, error)

	// StateSyncGetSummary retrieves the summary related to key, if available.
	// Returns ErrUnknownStateSummary if summary is not available
	StateSyncGetSummary(summaryKey uint64) (Summary, error)

	// StateSync is called with a list of valid summaries to sync from.
	// These summaries were collected from peers and validated with validators.
	// VM will use information inside the summary to choose one and sync
	// its state to that summary.
	// Will be called with an empty list if no valid state summaries could be found.
	// Normal bootstrapping resumes after VM signals the state sync process has completed.
	StateSync([]Summary) error
}
