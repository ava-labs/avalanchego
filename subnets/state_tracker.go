// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

// StateTracker describes the standard interface for tracking the status of
// a subnet syncing
type StateTracker interface {
	// Returns true iff all subnet chains have complete syncing
	IsSynced() bool

	SetState(chainID ids.ID, state snow.State)
	GetState(chainID ids.ID) snow.State

	OnSyncCompleted() chan struct{}
}
