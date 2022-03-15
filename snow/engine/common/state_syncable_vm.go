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
)

// Summary represents the information needed for state sync processing
type (
	SummaryKey  uint64
	SummaryHash ids.ID

	Summary interface {
		Bytes() []byte
		Key() SummaryKey
		Hash() SummaryHash
	}
)

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM
	StateSyncEnabled() (bool, error)

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
