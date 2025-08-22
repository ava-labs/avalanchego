// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import "github.com/ava-labs/avalanchego/ids"

// TestListener implements StateCallbackListener for testing
type TestListener struct {
	AddedValidators   map[ids.ID]ids.NodeID
	RemovedValidators map[ids.ID]ids.NodeID
	StatusUpdates     map[ids.ID]bool
}

func NewTestListener() *TestListener {
	return &TestListener{
		AddedValidators:   make(map[ids.ID]ids.NodeID),
		RemovedValidators: make(map[ids.ID]ids.NodeID),
		StatusUpdates:     make(map[ids.ID]bool),
	}
}

func (l *TestListener) OnValidatorAdded(vID ids.ID, nodeID ids.NodeID, startTime uint64, isActive bool) {
	l.AddedValidators[vID] = nodeID
}

func (l *TestListener) OnValidatorRemoved(vID ids.ID, nodeID ids.NodeID) {
	l.RemovedValidators[vID] = nodeID
}

func (l *TestListener) OnValidatorStatusUpdated(vID ids.ID, nodeID ids.NodeID, isActive bool) {
	l.StatusUpdates[vID] = isActive
}
