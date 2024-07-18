// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import "github.com/ava-labs/avalanchego/utils/math"

const (
	Bandwidth Dimension = iota // bytes
	DBRead                     // num reads
	DBWrite                    // num writes (includes deletes)
	Compute                    // time

	NumDimensions = iota
)

type (
	Dimension  uint
	Dimensions [NumDimensions]uint64
)

func (d Dimensions) Add(o Dimensions) (Dimensions, error) {
	var (
		res Dimensions
		err error
	)
	for i := range d {
		res[i], err = math.Add64(d[i], o[i])
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (d Dimensions) Sub(o Dimensions) (Dimensions, error) {
	var (
		res Dimensions
		err error
	)
	for i := range d {
		res[i], err = math.Sub(d[i], o[i])
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

func (d Dimensions) ToGas(weights Dimensions) (Gas, error) {
	var res uint64
	for i := range d {
		v, err := math.Mul64(d[i], weights[i])
		if err != nil {
			return 0, err
		}
		res, err = math.Add64(res, v)
		if err != nil {
			return 0, err
		}
	}
	return Gas(res), nil
}
