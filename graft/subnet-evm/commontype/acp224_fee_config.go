// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"errors"
	"fmt"
)

const MinTargetGasACP224 uint64 = 1_000_000

// ACP224FeeConfig specifies the parameters for the ACP-224 dynamic gas limit mechanism.
// See Verify() for validation constraints between fields.
type ACP224FeeConfig struct {
	ValidatorTargetGas bool   `json:"validatorTargetGas,omitempty"` // when true, validators control targetGas via node preferences
	TargetGas          uint64 `json:"targetGas"`                    // target gas consumption per second
	StaticPricing      bool   `json:"staticPricing,omitempty"`      // when true, gas price is always minGasPrice
	MinGasPrice        uint64 `json:"minGasPrice"`                  // minimum gas price in wei
	TimeToDouble       uint64 `json:"timeToDouble"`                 // seconds for gas price to double at max capacity
}

var (
	ErrMinGasPriceTooLow      = errors.New("minGasPrice must be greater than 0")
	ErrTargetGasMustBeZero    = errors.New("targetGas must be 0 when validatorTargetGas is true")
	ErrTargetGasTooLowACP224  = fmt.Errorf("targetGas must be at least %d", MinTargetGasACP224)
	ErrTimeToDoubleTooLow     = errors.New("timeToDouble must be greater than 0")
	ErrTimeToDoubleMustBeZero = errors.New("timeToDouble must be 0 when staticPricing is true")
)

// Verify returns an error if the config violates any field constraints.
func (a *ACP224FeeConfig) Verify() error {
	switch {
	case a.MinGasPrice == 0:
		return ErrMinGasPriceTooLow
	case a.ValidatorTargetGas && a.TargetGas != 0:
		return ErrTargetGasMustBeZero
	case !a.ValidatorTargetGas && a.TargetGas < MinTargetGasACP224:
		return ErrTargetGasTooLowACP224
	case a.StaticPricing && a.TimeToDouble != 0:
		return ErrTimeToDoubleMustBeZero
	case !a.StaticPricing && a.TimeToDouble == 0:
		return ErrTimeToDoubleTooLow
	default:
		return nil
	}
}

// Equal returns true if both configs are nil or have identical field values.
func (a *ACP224FeeConfig) Equal(other *ACP224FeeConfig) bool {
	if a == nil || other == nil {
		return a == other
	}

	return *a == *other
}
