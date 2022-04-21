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

// Summary represents all the information needed for state sync processing.
// Summary must allow a VM to download, verify and rebuild its state,
// no matter whether it is freshly created or it has previous state.
// Both Height and ID uniquely identify a Summary. However:
// Height is used to efficiently elicit network votes;
// ID must allow summaries comparison and verification as an alternative to Bytes;
// it is used to verify what summaries votes are casted for.
// Finally Byte returns the Summary content which is defined by the VM and opaque to the engine.
type Summary interface {
	Bytes() []byte
	Height() uint64
	ID() ids.ID

	Accept() error
}

// StateSyncableVM represents functionalities to allow VMs to sync to a given state,
// rather then boostrapping from genesis.
// common.StateSyncableVM can be detailed for Snowman or Avalanche-like VMs by extending the interface.
type StateSyncableVM interface {
	// StateSyncEnabled indicates whether the state sync is enabled for this VM.
	// If StateSyncableVM is not really implemented (as it may happen with rpcchainvm)
	// StateSyncEnabled should return false, nil
	StateSyncEnabled() (bool, error)

	// GetOngoingStateSyncSummary returns an in-progress state summary if it exists. This
	// allows the engine to ask the network if the ongoing summary is still supported by the
	// network. This simplifies the task of the StateSyncableVM to decide whether to
	// continue an in-progress sync or start over.
	// If no local state summary exists, GetOngoingStateSyncSummary returns an
	// Empty Summary with empty ID, zero height, and nil bytes.
	GetOngoingStateSyncSummary() (Summary, error)

	// GetLastStateSummary returns latest Summary with an optional error
	// Returns ErrUnknownStateSummary if summary is not available
	GetLastStateSummary() (Summary, error)

	// ParseStateSummary builds a Summary out of summaryBytes
	ParseStateSummary(summaryBytes []byte) (Summary, error)

	// GetStateSummary retrieves the summary related to height, if available.
	// Returns ErrUnknownStateSummary if summary is not available
	GetStateSummary(summaryHeight uint64) (Summary, error)
}
