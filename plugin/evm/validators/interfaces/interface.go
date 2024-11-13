// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interfaces

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/set"
)

type State interface {
	uptime.State
	// AddValidator adds a new validator to the state
	AddValidator(vdr Validator) error
	// UpdateValidator updates the validator in the state
	UpdateValidator(vdr Validator) error
	// GetValidator returns the validator data for the given validation ID
	GetValidator(vID ids.ID) (Validator, error)
	// DeleteValidator deletes the validator from the state
	DeleteValidator(vID ids.ID) error
	// WriteState writes the validator state to the disk
	WriteState() error

	// GetValidationIDs returns the validation IDs in the state
	GetValidationIDs() set.Set[ids.ID]
	// GetNodeIDs returns the validator node IDs in the state
	GetNodeIDs() set.Set[ids.NodeID]

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

type Validator struct {
	ValidationID   ids.ID     `json:"validationID"`
	NodeID         ids.NodeID `json:"nodeID"`
	Weight         uint64     `json:"weight"`
	StartTimestamp uint64     `json:"startTimestamp"`
	IsActive       bool       `json:"isActive"`
	IsSoV          bool       `json:"isSoV"`
}

func (v *Validator) StartTime() time.Time { return time.Unix(int64(v.StartTimestamp), 0) }
