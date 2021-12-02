// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
)

const (
	CostPerSignature uint64 = 1000
)

var (
	errNilInput        = errors.New("nil input")
	errNotSortedUnique = errors.New("signatures not sorted and unique")
)

type Input struct {
	// This input consumes an output, which has an owner list.
	// This input will be spent with a list of signatures.
	// SignatureList[i] is the signature of OwnerList[i]
	SigIndices []uint32 `serialize:"true" json:"signatureIndices"`
}

func (in *Input) Cost() (uint64, error) {
	numSigs := uint64(len(in.SigIndices))
	return math.Mul64(numSigs, CostPerSignature)
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
