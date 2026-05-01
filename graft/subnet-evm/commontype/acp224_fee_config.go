// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"encoding/binary"
	"errors"

	"github.com/ava-labs/libevm/common"
)

const MinTargetGasACP224 uint64 = 1_000_000

// DefaultACP224FeeConfig returns a default ACP-224 fee
// config.
//
// The default values are:
//   - TargetGas: 1_000_000
//   - MinGasPrice: 1
//   - TimeToDouble: 60
func DefaultACP224FeeConfig() ACP224FeeConfig {
	return ACP224FeeConfig{
		TargetGas:    1_000_000,
		MinGasPrice:  1,
		TimeToDouble: 60,
	}
}

var (
	ErrMinGasPriceTooLow = errors.New("minGasPrice must be greater than 0")

	errTargetGasMustBeZero    = errors.New("targetGas must be 0 when validatorTargetGas is true")
	errTargetGasTooLowACP224  = errors.New("targetGas must be at least MinTargetGasACP224")
	errTimeToDoubleTooLow     = errors.New("timeToDouble must be greater than 0")
	errTimeToDoubleMustBeZero = errors.New("timeToDouble must be 0 when staticPricing is true")
)

// ACP224FeeConfig specifies the parameters for the ACP-224 dynamic gas limit mechanism.
// See [ACP224FeeConfig.Verify] for validation constraints between fields.
type ACP224FeeConfig struct {
	ValidatorTargetGas bool   `json:"validatorTargetGas,omitempty"` // when true, validators control targetGas via node preferences
	TargetGas          uint64 `json:"targetGas"`                    // target gas consumption per second
	StaticPricing      bool   `json:"staticPricing,omitempty"`      // when true, gas price is always minGasPrice
	MinGasPrice        uint64 `json:"minGasPrice"`                  // minimum gas price in wei
	TimeToDouble       uint64 `json:"timeToDouble"`                 // seconds for gas price to double at max capacity
}

// Verify returns an error if the config violates any field constraints.
func (a *ACP224FeeConfig) Verify() error {
	switch {
	case a.MinGasPrice == 0:
		return ErrMinGasPriceTooLow
	case a.ValidatorTargetGas && a.TargetGas != 0:
		return errTargetGasMustBeZero
	case !a.ValidatorTargetGas && a.TargetGas < MinTargetGasACP224:
		return errTargetGasTooLowACP224
	case a.StaticPricing && a.TimeToDouble != 0:
		return errTimeToDoubleMustBeZero
	case !a.StaticPricing && a.TimeToDouble == 0:
		return errTimeToDoubleTooLow
	default:
		return nil
	}
}

// Pack encodes the fee config into a single common.Hash (32 bytes).
//
// Layout (26 bytes used, 6 bytes padding):
//
//	h[0]     ValidatorTargetGas (bool)
//	h[1:9]   TargetGas          (uint64)
//	h[9]     StaticPricing      (bool)
//	h[10:18] MinGasPrice        (uint64)
//	h[18:26] TimeToDouble       (uint64)
func (a *ACP224FeeConfig) Pack() common.Hash {
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

// UnpackFrom decodes a packed common.Hash into the fee config fields.
// See [ACP224FeeConfig.Pack] for the byte layout.
func (a *ACP224FeeConfig) UnpackFrom(h common.Hash) {
	u64 := binary.BigEndian.Uint64

	a.ValidatorTargetGas = h[0] != 0
	a.TargetGas = u64(h[1:])
	a.StaticPricing = h[9] != 0
	a.MinGasPrice = u64(h[10:])
	a.TimeToDouble = u64(h[18:])
}

// Equal returns true if both configs are nil or have identical field values.
func (a *ACP224FeeConfig) Equal(other *ACP224FeeConfig) bool {
	if a == nil || other == nil {
		return a == other
	}

	return *a == *other
}
