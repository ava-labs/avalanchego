// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"encoding/binary"
	"errors"

	"github.com/ava-labs/libevm/common"
)

const MinTargetGas uint64 = 1_000_000

// DefaultGasPriceConfig returns a default gas price config for the dynamic
// gas limit mechanism.
//
// The default values are:
//   - TargetGas: 1_000_000
//   - MinGasPrice: 1
//   - TimeToDouble: 60
func DefaultGasPriceConfig() GasPriceConfig {
	return GasPriceConfig{
		TargetGas:    1_000_000,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}
}

var (
	ErrMinGasPriceTooLow = errors.New("minGasPrice must be greater than 0")

	errTargetGasMustBeZero    = errors.New("targetGas must be 0 when validatorTargetGas is true")
	errTargetGasBelowMin      = errors.New("targetGas must be at least MinTargetGas")
	errTimeToDoubleTooLow     = errors.New("timeToDouble must be greater than 0")
	errTimeToDoubleMustBeZero = errors.New("timeToDouble must be 0 when staticPricing is true")
)

// GasPriceConfig specifies the parameters for the dynamic gas limit and
// gas price mechanism.
// See [GasPriceConfig.Verify] for validation constraints between fields.
type GasPriceConfig struct {
	ValidatorTargetGas bool   `json:"validatorTargetGas"` // when true, validators control targetGas via node preferences
	TargetGas          uint64 `json:"targetGas"`          // target gas consumption per second
	StaticPricing      bool   `json:"staticPricing"`      // when true, gas price is always minGasPrice
	MinGasPrice        uint64 `json:"minGasPrice"`        // minimum gas price in wei
	TimeToDouble       uint64 `json:"timeToDouble"`       // seconds for gas price to double at max capacity
}

// Verify returns an error if the config violates any field constraints.
func (a *GasPriceConfig) Verify() error {
	switch {
	case a.MinGasPrice == 0:
		return ErrMinGasPriceTooLow
	case a.ValidatorTargetGas && a.TargetGas != 0:
		return errTargetGasMustBeZero
	case !a.ValidatorTargetGas && a.TargetGas < MinTargetGas:
		return errTargetGasBelowMin
	case a.StaticPricing && a.TimeToDouble != 0:
		return errTimeToDoubleMustBeZero
	case !a.StaticPricing && a.TimeToDouble == 0:
		return errTimeToDoubleTooLow
	default:
		return nil
	}
}

// Pack encodes the gas price config into a single common.Hash (32 bytes).
//
// Layout (26 bytes used, 6 bytes padding):
//
//	h[0]     ValidatorTargetGas (bool)
//	h[1:9]   TargetGas          (uint64)
//	h[9]     StaticPricing      (bool)
//	h[10:18] MinGasPrice        (uint64)
//	h[18:26] TimeToDouble       (uint64)
func (a *GasPriceConfig) Pack() common.Hash {
	var h common.Hash
	put := binary.BigEndian.PutUint64

	if a.ValidatorTargetGas {
		h[0] = 1
	}
	put(h[1:], a.TargetGas)
	if a.StaticPricing {
		h[9] = 1
	}
	put(h[10:], a.MinGasPrice)
	put(h[18:], a.TimeToDouble)
	return h
}

// UnpackFrom decodes a packed common.Hash into the gas price config fields.
// See [GasPriceConfig.Pack] for the byte layout.
func (a *GasPriceConfig) UnpackFrom(h common.Hash) {
	u64 := binary.BigEndian.Uint64

	a.ValidatorTargetGas = h[0] != 0
	a.TargetGas = u64(h[1:])
	a.StaticPricing = h[9] != 0
	a.MinGasPrice = u64(h[10:])
	a.TimeToDouble = u64(h[18:])
}

// Equal returns true if both configs are nil or have identical field values.
func (a *GasPriceConfig) Equal(other *GasPriceConfig) bool {
	if a == nil || other == nil {
		return a == other
	}

	return *a == *other
}
