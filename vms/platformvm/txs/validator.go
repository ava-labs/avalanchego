// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	ErrWeightTooSmall = errors.New("weight of this validator is too low")
	errBadSubnetID    = errors.New("subnet ID can't be primary network ID")
)

// Validator is a validator.
type Validator struct {
	// Node ID of the validator
	NodeID ids.NodeID `serialize:"true" json:"nodeID"`

	// Unix time this validator starts validating
	Start uint64 `serialize:"true" json:"start"`

	// Unix time this validator stops validating
	End uint64 `serialize:"true" json:"end"`

	// Weight of this validator used when sampling
	Wght uint64 `serialize:"true" json:"weight"`
}

// StartTime is the time that this validator will enter the validator set
func (v *Validator) StartTime() time.Time {
	return time.Unix(int64(v.Start), 0)
}

// EndTime is the time that this validator will leave the validator set
func (v *Validator) EndTime() time.Time {
	return time.Unix(int64(v.End), 0)
}

// Weight is this validator's weight when sampling
func (v *Validator) Weight() uint64 {
	return v.Wght
}

// Verify validates the ID for this validator
func (v *Validator) Verify() error {
	// Ensure the validator has some weight
	if v.Wght == 0 {
		return ErrWeightTooSmall
	}

	return nil
}

// BoundedBy returns true iff staker start and end are a
// (non-strict) subset of the provided time bound
func BoundedBy(stakerStart, stakerEnd, lowerBound, upperBound time.Time) bool {
	return !stakerStart.Before(lowerBound) && !stakerEnd.After(upperBound) && !stakerEnd.Before(stakerStart)
}
