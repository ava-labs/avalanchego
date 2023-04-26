// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var (
	ErrWeightTooSmall       = errors.New("weight of this validator is too low")
	ErrBadValidatorDuration = errors.New("validator duration too large")
	errBadSubnetID          = errors.New("subnet ID can't be primary network ID")
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
	if v.Start == 0 {
		return mockable.MaxTime
	}
	return time.Unix(int64(v.End), 0)
}

// StakingPeriod is the amount of time that this validator will be in the validator set
func (v *Validator) StakingPeriod() time.Duration {
	if v.Start == 0 {
		return time.Duration(v.End) * time.Second
	}
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
	case v.Start == 0 && v.StakingPeriod() > StakerMaxDuration:
		// Ensure proper encoding when v.End is used to encode a duration
		return ErrBadValidatorDuration
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
