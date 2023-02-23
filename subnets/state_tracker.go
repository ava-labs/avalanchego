// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"github.com/ava-labs/avalanchego/ids"
)

// StateTracker describes the standard interface for tracking the status of
// a subnet syncing
type StateTracker interface {
	// Returns true iff done bootstrapping
	IsSynced() bool

	// Bootstrapped marks the named chain as being bootstrapped
	Bootstrapped(chainID ids.ID)

	OnSyncCompleted() chan struct{}
}
