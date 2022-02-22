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
type Key struct {
	Content []byte `serialize:"true"` // actual state key content
}

type Summary struct {
	Content []byte `serialize:"true"` // actual state summary content
}

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// RegisterStateSyncer registers nodes from which state sync summaries can be downloaded
	RegisterStateSyncer(stateSyncer []ids.ShortID) error

	// StateSyncEnabled indicates whether the state sync is enabled for this VM
	StateSyncEnabled() (bool, error)

	// StateSyncGetLastSummary returns latest Summary with an optional error
	StateSyncGetLastSummary() (Summary, error)

	// StateSyncGetKey retrieves a summary key out of a summary
	StateSyncGetKey(Summary) (Key, error)

	// StateSyncCheckPair checks whether a locally available key is valid and matches Summary
	StateSyncCheckPair(Key, Summary) (bool, error)

	// StateSync is called with a list of valid summaries to sync from.
	// These summaries were collected from peers and validated with validators.
	// VM will use information inside the summary to choose one and sync
	// its state to that summary. Normal bootstrapping resumes after this
	// function returns.
	// Will be called with [nil] if no valid state summaries could be found.
	StateSync([]Summary) error
}
