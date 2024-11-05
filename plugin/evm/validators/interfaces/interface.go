// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/set"
)

type State interface {
	uptime.State
	// AddValidator adds a new validator to the state
	AddValidator(vID ids.ID, nodeID ids.NodeID, startTimestamp uint64, isActive bool) error
	// DeleteValidator deletes the validator from the state
	DeleteValidator(vID ids.ID) error
	// WriteState writes the validator state to the disk
	WriteState() error

	// SetStatus sets the active status of the validator with the given vID
	SetStatus(vID ids.ID, isActive bool) error
	// GetStatus returns the active status of the validator with the given vID
	GetStatus(vID ids.ID) (bool, error)

	// GetValidationIDs returns the validation IDs in the state
	GetValidationIDs() set.Set[ids.ID]
	// GetValidatorIDs returns the validator node IDs in the state
	GetValidatorIDs() set.Set[ids.NodeID]

	// RegisterListener registers a listener to the state
	RegisterListener(StateCallbackListener)
}

// StateCallbackListener is a listener for the validator state
type StateCallbackListener interface {
	// OnValidatorAdded is called when a new validator is added
	OnValidatorAdded(vID ids.ID, nodeID ids.NodeID, startTime uint64, isActive bool)
	// OnValidatorRemoved is called when a validator is removed
	OnValidatorRemoved(vID ids.ID, nodeID ids.NodeID)
	// OnValidatorStatusUpdated is called when a validator status is updated
	OnValidatorStatusUpdated(vID ids.ID, nodeID ids.NodeID, isActive bool)
}
