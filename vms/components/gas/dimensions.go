// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gas

import "github.com/ava-labs/avalanchego/utils/math"

const (
	Bandwidth Dimension = iota
	DBRead
	DBWrite // includes deletes
	Compute

	NumDimensions = iota
)

type (
	Dimension  uint
	Dimensions [NumDimensions]uint64
)

// Add returns d + sum(os...).
//
// If overflow occurs, an error is returned.
func (d Dimensions) Add(os ...*Dimensions) (Dimensions, error) {
	var err error
	for _, o := range os {
		for i := range o {
			d[i], err = math.Add(d[i], o[i])
			if err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

// Sub returns d - sum(os...).
//
// If underflow occurs, an error is returned.
func (d Dimensions) Sub(os ...*Dimensions) (Dimensions, error) {
	var err error
	for _, o := range os {
		for i := range o {
			d[i], err = math.Sub(d[i], o[i])
			if err != nil {
				return d, err
			}
		}
	}
	return d, nil
}

// ToGas returns d · weights.
//
// If overflow occurs, an error is returned.
func (d Dimensions) ToGas(weights Dimensions) (Gas, error) {
	var res uint64
	for i := range d {
		v, err := math.Mul(d[i], weights[i])
		if err != nil {
			return 0, err
		}
		res, err = math.Add(res, v)
		if err != nil {
			return 0, err
		}
	}
	return Gas(res), nil
}
