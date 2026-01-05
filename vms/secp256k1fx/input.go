// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	ErrNilInput                    = errors.New("nil input")
	ErrInputIndicesNotSortedUnique = errors.New("address indices not sorted and unique")
)

type Input struct {
	// This input consumes an output, which has an owner list.
	// This input will be spent with a list of signatures.
	// SignatureList[i] is the signature of OwnerList[i]
	SigIndices []uint32 `serialize:"true" json:"signatureIndices"`
}

func (in *Input) Cost() (uint64, error) {
	numSigs := uint64(len(in.SigIndices))
	return math.Mul(numSigs, CostPerSignature)
}

// Verify this input is syntactically valid
func (in *Input) Verify() error {
	switch {
	case in == nil:
		return ErrNilInput
	case !utils.IsSortedAndUniqueOrdered(in.SigIndices):
		return ErrInputIndicesNotSortedUnique
	default:
		return nil
	}
}
