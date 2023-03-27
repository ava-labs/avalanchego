// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
)

// SubnetStateTracker describes the standard interface for tracking the status of
// a subnet syncing
type SubnetStateTracker interface {
	// IsSynced returns true iff all subnet chains have complete syncing.
	// A subnet is fully synced if all chains have completed bootstrap
	// and state synced chains have completed state sync too-
	IsSynced() bool

	// IsChainBootstrapped return true if chainID has currently completed
	// state syncing and bootstrapping, even if the whole subnet is still to
	// fully sync.
	IsChainBootstrapped(chainID ids.ID) bool

	// GetState returns latest started state for chainID
	GetState(chainID ids.ID) (State, p2p.EngineType)

	StartState(chainID ids.ID, state State, currentEngineType p2p.EngineType)
	StopState(chainID ids.ID, state State)

	// one sync channel per chain, to make sure each chain gets notified
	// when subnet completes sync
	OnSyncCompleted(chainID ids.ID) (chan struct{}, error)
}
