// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/ava-labs/avalanchego/ids"
)

// SubnetStateTracker describes the standard interface for tracking the status of
// a subnet syncing
type SubnetStateTracker interface {
	// Returns true iff all subnet chains have complete syncing
	IsSynced() bool

	SetState(chainID ids.ID, state State)
	GetState(chainID ids.ID) State

	OnSyncCompleted() chan struct{}
}
