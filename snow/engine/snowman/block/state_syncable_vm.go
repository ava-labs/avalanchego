// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
)

// [StateSummaryMode] is returned by the StateSyncableVM one a state summary is passed to it.
// [StateSummaryMode] indicates whuch type of state sync to use.
type StateSummaryMode uint8

const (
	// [StateSummaryStopped] indicates that state sync won't be really run by the VM
	// (e.g. state sync is too recent, it's faster to bootstrap missing blocks)
	StateSummaryStopped StateSummaryMode = iota + 1

	// [StateSummaryStatic] indicates that engine should stop and wait for state sync
	// to complete before moving ahead with bootstrapping.
	StateSummaryStatic

	// [StateSummaryDynamic] indicates that engine should keep going with bootstrapping
	// and normal engine mode. State sync will proceed asynchronously in VM.
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
