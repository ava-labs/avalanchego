// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils"
)

var (
	errNilInput        = errors.New("nil input")
	errNotSortedUnique = errors.New("signatures not sorted and unique")
)

// Input ...
type Input struct {
	// This input consumes an output, which has an owner list.
	// This input will be spent with a list of signatures.
	// SignatureList[i] is the signature of OwnerList[i]
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
