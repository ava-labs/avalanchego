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

// Summary represents the information needed for state sync processing
type (
	SummaryKey uint64
	SummaryID  ids.ID

	Summary interface {
		Bytes() []byte
		Key() SummaryKey
		ID() SummaryID
	}
)

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM
	StateSyncEnabled() (bool, error)

	// A State sync may have been initiate in a previous node run. In such case
	// GetOngoingStateSyncSummary allows retrieving the state summary for the ongoing
	// state sync, so that network availability of the summary can be inspected. This allows
	// StateSyncableVM to decide whether to continue syncing the same summary or restarting
	// from a different one.
	// GetOngoingStateSyncSummary should return ErrNoStateSyncOngoing is not previous
	// state sync should be inspected.
	GetOngoingStateSyncSummary() (Summary, error)

	// StateSyncGetLastSummary returns latest Summary with an optional error
	StateSyncGetLastSummary() (Summary, error)

	// ParseSummary builds a Summary out of summaryBytes
	ParseSummary(summaryBytes []byte) (Summary, error)

	// StateSyncGetSummary retrieves the summary related to key, if available.
	StateSyncGetSummary(SummaryKey) (Summary, error)

	// StateSync is called with a list of valid summaries to sync from.
	// These summaries were collected from peers and validated with validators.
	// VM will use information inside the summary to choose one and sync
	// its state to that summary. Normal bootstrapping resumes after this
	// function returns.
	// Will be called with [nil] if no valid state summaries could be found.
	StateSync([]Summary) error
}
