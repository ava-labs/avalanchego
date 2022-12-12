// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

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

// ID returns the node ID of the validator
func (v *Validator) ID() ids.NodeID {
	return v.NodeID
}

// StartTime is the time that this validator will enter the validator set
func (v *Validator) StartTime() time.Time {
	return time.Unix(int64(v.Start), 0)
}

// EndTime is the time that this validator will leave the validator set
func (v *Validator) EndTime() time.Time {
	return time.Unix(int64(v.End), 0)
}

// Duration is the amount of time that this validator will be in the validator set
func (v *Validator) Duration() time.Duration {
	return v.EndTime().Sub(v.StartTime())
}

// Weight is this validator's weight when sampling
func (v *Validator) Weight() uint64 {
	return v.Wght
}

// Verify validates the ID for this validator
func (v *Validator) Verify() error {
	switch {
	case v.Wght == 0: // Ensure the validator has some weight
		return ErrWeightTooSmall
	default:
		return nil
	}
}

// BoundedBy returns true iff the period that [validator] validates is a
// (non-strict) subset of the time that [other] validates.
// Namely, startTime <= v.StartTime() <= v.EndTime() <= endTime
func (v *Validator) BoundedBy(startTime, endTime time.Time) bool {
	return !v.StartTime().Before(startTime) && !v.EndTime().After(endTime)
}
