// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/gecko/utils"
)

var (
	errNilInput        = errors.New("nil input")
	errNotSortedUnique = errors.New("signatures not sorted and unique")
)

// Input ...
type Input struct {
	SigIndices []uint32 `serialize:"true" json:"signatureIndices"`
}

// Verify this input is syntactically valid
func (in *Input) Verify() error {
	switch {
	case in == nil:
		return errNilInput
	case !utils.IsSortedAndUniqueUint32(in.SigIndices):
		return errNotSortedUnique
	default:
		return nil
	}
}
