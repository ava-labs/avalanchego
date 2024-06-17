// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"encoding/binary"
	"errors"
	"fmt"

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

	uint64Len = 8
)

var (
	errUnknownDimension = errors.New("unknown dimension")

	ZeroGas      = Gas(0)
	ZeroGasPrice = GasPrice(0)
	Empty        = Dimensions{}

	DimensionStrings = []string{
		bandwidthString,
		utxosReadString,
		utxosWriteString,
		computeString,
	}
)

type (
	GasPrice uint64
	Gas      uint64

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

func ScalarProd(lhs, rhs Dimensions) (Gas, error) {
	var res uint64
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Mul64(lhs[i], rhs[i])
		if err != nil {
			return ZeroGas, err
		}
		res, err = safemath.Add64(res, v)
		if err != nil {
			return ZeroGas, err
		}
	}
	return Gas(res), nil
}

// [Compare] returns true only if rhs[i] >= lhs[i] for each dimensions
// Arrays ordering is not total, so we avoided naming [Compare] as [Less]
// to discourage improper use
func Compare(lhs, rhs Dimensions) bool {
	for i := 0; i < FeeDimensions; i++ {
		if lhs[i] > rhs[i] {
			return false
		}
	}
	return true
}

func (d *Dimensions) Bytes() []byte {
	res := make([]byte, FeeDimensions*uint64Len)
	for i := Dimension(0); i < FeeDimensions; i++ {
		binary.BigEndian.PutUint64(res[i*uint64Len:], d[i])
	}
	return res
}

func (d *Dimensions) FromBytes(b []byte) error {
	if len(b) != FeeDimensions*uint64Len {
		return fmt.Errorf("unexpected bytes length: expected %d, actual %d",
			FeeDimensions*uint64Len,
			len(b),
		)
	}
	for i := Dimension(0); i < FeeDimensions; i++ {
		d[i] = binary.BigEndian.Uint64(b[i*uint64Len : (i+1)*uint64Len])
	}
	return nil
}
