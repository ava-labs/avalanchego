// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"math"

	"github.com/ava-labs/gecko/ids"
	safemath "github.com/ava-labs/gecko/utils/math"
)

// Validator is the minimal description of someone that can be sampled.
type Validator interface {
	// ID returns the node ID of this validator
	ID() ids.ShortID

	// Returns this validator's weight
	Weight() uint64
}

type validator struct {
	id     ids.ShortID
	weight uint64
}

func (v *validator) ID() ids.ShortID {
	return v.id
}

func (v *validator) addWeight(weight uint64) {
	newTotalWeight, err := safemath.Add64(weight, v.weight)
	if err != nil {
		newTotalWeight = math.MaxUint64
	}
	v.weight = newTotalWeight
}

func (v *validator) removeWeight(weight uint64) {
	newTotalWeight, err := safemath.Sub64(v.weight, weight)
	if err != nil {
		newTotalWeight = 0
	}
	v.weight = newTotalWeight
}

func (v *validator) Weight() uint64 {
	return v.weight
}
