// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/gecko/ids"
)

// testValidator is a struct that contains the base values required by the
// validator interface. This struct is used only for testing.
type testValidator struct {
	id     ids.ShortID
	weight uint64
}

func (v *testValidator) ID() ids.ShortID { return v.id }
func (v *testValidator) Weight() uint64  { return v.weight }

// NewValidator returns a validator object that implements the Validator
// interface
func NewValidator(id ids.ShortID, weight uint64) Validator {
	return &testValidator{
		id:     id,
		weight: weight,
	}
}

var (
	vdrOffset = uint64(0)
)

// GenerateRandomValidator creates a random validator with the provided weight
func GenerateRandomValidator(weight uint64) Validator {
	vdrOffset++
	id := ids.Empty.Prefix(vdrOffset)
	bytes := id.Bytes()
	hash := [20]byte{}
	copy(hash[:], bytes)
	return NewValidator(ids.NewShortID(hash), weight)
}
