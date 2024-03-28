// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"errors"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	Bandwidth Dimension = 0
	UTXORead  Dimension = 1
	UTXOWrite Dimension = 2 // includes delete
	Compute   Dimension = 3 // signatures checks, tx-specific

	bandwidthString  string = "Bandwidth"
	utxosReadString  string = "UTXOsRead"
	utxosWriteString string = "UTXOsWrite"
	computeString    string = "Compute"

	FeeDimensions = 4
)

var (
	errUnknownDimension = errors.New("unknown dimension")

	Empty = Dimensions{}
	Max   = Dimensions{
		math.MaxUint64,
		math.MaxUint64,
		math.MaxUint64,
		math.MaxUint64,
	}

	DimensionStrings = []string{
		bandwidthString,
		utxosReadString,
		utxosWriteString,
		computeString,
	}
)

type (
	Dimension  int
	Dimensions [FeeDimensions]uint64
)

func (d Dimension) String() (string, error) {
	if d < 0 || d >= FeeDimensions {
		return "", errUnknownDimension
	}

	return DimensionStrings[d], nil
}

func Add(lhs, rhs Dimensions) (Dimensions, error) {
	var res Dimensions
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Add64(lhs[i], rhs[i])
		if err != nil {
			return res, err
		}
		res[i] = v
	}
	return res, nil
}
