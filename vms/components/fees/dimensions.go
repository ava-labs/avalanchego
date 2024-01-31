// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import "github.com/ava-labs/avalanchego/utils/math"

const (
	Bandwidth Dimension = 0
	UTXORead  Dimension = 1
	UTXOWrite Dimension = 2 // includes delete
	Compute   Dimension = 3 // signatures checks, tx-specific

	FeeDimensions = 4
)

var (
	EmptyUnitFees = Dimensions{} // helps avoiding reading unit fees from db for some pre E fork processing
	EmptyUnitCaps = Dimensions{} // helps avoiding reading unit fees from db for some pre E fork processing
	EmptyWindows  = [FeeDimensions]Window{}
)

type (
	Dimension  int
	Dimensions [FeeDimensions]uint64
)

func Add(lhs, rhs Dimensions) (Dimensions, error) {
	var res Dimensions
	for i := 0; i < FeeDimensions; i++ {
		v, err := math.Add64(lhs[i], rhs[i])
		if err != nil {
			return res, err
		}
		res[i] = v
	}
	return res, nil
}
