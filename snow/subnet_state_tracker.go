// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/ava-labs/avalanchego/ids"
)

// SubnetStateTracker describes the standard interface for tracking the status of
// a subnet syncing
type SubnetStateTracker interface {
	// IsSynced returns true iff all subnet chains have complete syncing.
	// A subnet is fully synced if all chains have completed bootstrap
	// and state synced chains have completed state sync too-
	IsSynced() bool

	StartState(chainID ids.ID, state State)
	StopState(chainID ids.ID, state State)

	// IsChainBootstrapped return true if chainID has currently completed
	// state syncing and bootstrapping, even if the whole subnet is still to
	// fully sync.
	IsChainBootstrapped(chainID ids.ID) bool

	GetState(chainID ids.ID) State

	OnSyncCompleted() chan struct{}
}
