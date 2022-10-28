// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"

	"github.com/ava-labs/avalanchego/ids"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var _ Validator = (*validator)(nil)

// Validator is the minimal description of someone that can be sampled.
type Validator interface {
	// ID returns the node ID of this validator
	ID() ids.NodeID

	// Weight that can be used for weighted sampling. If this validator is
	// validating the primary network, returns the amount of AVAX staked.
	Weight() uint64
}

// validator is a struct that contains the base values required by the validator
// interface.
type validator struct {
	nodeID ids.NodeID
	weight uint64
}

func (v *validator) ID() ids.NodeID { return v.nodeID }
func (v *validator) Weight() uint64 { return v.weight }

func (v *validator) addWeight(weight uint64) {
	newTotalWeight, err := safemath.Add64(weight, v.weight)
	if err != nil {
		newTotalWeight = math.MaxUint64
	}
	v.weight = newTotalWeight
}

func (v *validator) removeWeight(weight uint64) {
	newTotalWeight, err := safemath.Sub(v.weight, weight)
	if err != nil {
		newTotalWeight = 0
	}
	v.weight = newTotalWeight
}

// NewValidator returns a validator object that implements the Validator
// interface
func NewValidator(
	nodeID ids.NodeID,
	weight uint64,
) Validator {
	return &validator{
		nodeID: nodeID,
		weight: weight,
	}
}
