// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
)

// Validator ...
type Validator struct {
	// Node ID of the staker
	NodeID ids.ShortID `serialize:"true"`

	// Weight of this validator used when sampling
	Wght uint64 `serialize:"true"`
}

// ID returns the node ID of the staker
func (v *Validator) ID() ids.ShortID { return v.NodeID }

// Weight is this validator's weight when sampling
func (v *Validator) Weight() uint64 { return v.Wght }

// Vdr returns this validator
func (v *Validator) Vdr() validators.Validator { return v }

// DurationValidator ...
type DurationValidator struct {
	Validator `serialize:"true"`

	// Unix time this staker starts validating
	Start uint64 `serialize:"true"`

	// Unix time this staker stops validating
	End uint64 `serialize:"true"`
}

// StartTime is the time that this staker will enter the validator set
func (v *DurationValidator) StartTime() time.Time { return time.Unix(int64(v.Start), 0) }

// EndTime is the time that this staker will leave the validator set
func (v *DurationValidator) EndTime() time.Time { return time.Unix(int64(v.End), 0) }

// Duration is the amount of time that this staker will be in the validator set
func (v *DurationValidator) Duration() time.Duration { return v.EndTime().Sub(v.StartTime()) }

// BoundedBy returns true iff the period that [validator] validates is a
// (non-strict) subset of the time that [other] validates.
// Namely, startTime <= v.StartTime() <= v.EndTime() <= endTime
func (v *DurationValidator) BoundedBy(startTime, endTime time.Time) bool {
	return !v.StartTime().Before(startTime) && !v.EndTime().After(endTime)
}

// SubnetValidator validates a blockchain on the AVA network.
type SubnetValidator struct {
	DurationValidator `serialize:"true"`

	// ID of the subnet this validator is validating
	Subnet ids.ID `serialize:"true"`
}

// SubnetID is the ID of the subnet this validator is validating
func (v *SubnetValidator) SubnetID() ids.ID { return v.Subnet }
