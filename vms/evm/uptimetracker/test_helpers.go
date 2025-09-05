// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// TestPausableManager tracks callback invocations for testing
type TestPausableManager struct {
	*pausableManager
	AddedValidators   map[ids.ID]ids.NodeID
	RemovedValidators map[ids.ID]ids.NodeID
	StatusUpdates     map[ids.ID]bool
}

func NewTestPausableManager() *TestPausableManager {
	pm := &pausableManager{
		pausedVdrs:    make(set.Set[ids.NodeID]),
		connectedVdrs: make(set.Set[ids.NodeID]),
		// Manager will be nil for testing, but that's ok since we're only testing callbacks
	}

	return &TestPausableManager{
		pausableManager:   pm,
		AddedValidators:   make(map[ids.ID]ids.NodeID),
		RemovedValidators: make(map[ids.ID]ids.NodeID),
		StatusUpdates:     make(map[ids.ID]bool),
	}
}

// Override the callback methods to track invocations
func (t *TestPausableManager) OnValidatorAdded(vID ids.ID, nodeID ids.NodeID, startTime uint64, isActive bool) {
	t.AddedValidators[vID] = nodeID
}

func (t *TestPausableManager) OnValidatorRemoved(vID ids.ID, nodeID ids.NodeID) {
	t.RemovedValidators[vID] = nodeID
}

func (t *TestPausableManager) OnValidatorStatusUpdated(vID ids.ID, nodeID ids.NodeID, isActive bool) {
	t.StatusUpdates[vID] = isActive
}
