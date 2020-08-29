// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"time"

	"github.com/ava-labs/gecko/ids"
)

// Validator is the minimal description of someone that can be sampled.
type Validator interface {
	// ID returns the unique id of this validator
	ID() ids.ShortID

	// Weight that can be used for weighted sampling. If this validator is
	// validating the primary network, returns the amount of AVAX staked.
	Weight() uint64

	// Time this validator was supposed to start validating
	StartTime() time.Time

	// Time this validator was supposed to stop validating
	EndTime() time.Time
}

// validator is a struct that contains the base values required by the validator
// interface.
type testValidator struct {
	nodeID    ids.ShortID
	weight    uint64
	startTime time.Time
	endTime   time.Time
}

func (v *testValidator) ID() ids.ShortID      { return v.nodeID }
func (v *testValidator) Weight() uint64       { return v.weight }
func (v *testValidator) StartTime() time.Time { return v.startTime }
func (v *testValidator) EndTime() time.Time   { return v.endTime }

// NewValidator returns a validator object that implements the Validator
// interface
func NewValidator(
	nodeID ids.ShortID,
	weight uint64,
	startTime time.Time,
	endTime time.Time,
) Validator {
	return &testValidator{
		nodeID: nodeID,
		weight: weight,
	}
}

// GenerateRandomValidator creates a random validator with the provided weight
func GenerateRandomValidator(weight uint64) Validator {
	nodeID := ids.GenerateTestShortID()
	startTime := time.Now()
	endTime := startTime.Add(time.Hour)
	return NewValidator(
		nodeID,
		weight,
		startTime,
		endTime,
	)
}
