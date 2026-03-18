// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package commontype

import (
	"errors"
	"fmt"
)

const MinTargetGasACP224 uint64 = 1_000_000

// ACP224FeeConfig specifies the parameters for the ACP-224 dynamic gas limit mechanism.
// This configuration defines the target gas consumption, minimum gas price,
// and the time-based price adjustment mechanism.
type ACP224FeeConfig struct {
	// ValidatorTargetGas indicates whether the target gas is determined by the validator set.
	// When true, TargetGas must be 0.
	// When false, TargetGas must be >= MinTargetGasACP224.
	ValidatorTargetGas bool `json:"validatorTargetGas,omitempty"`

	// TargetGas specifies the target gas consumption per second.
	// Must be 0 when ValidatorTargetGas is true, >= MinTargetGasACP224 otherwise.
	TargetGas uint64 `json:"targetGas"`

	// StaticPricing indicates whether the gas price is static.
	// When true, TimeToDouble must be 0.
	// When false, TimeToDouble must be > 0.
	StaticPricing bool `json:"staticPricing,omitempty"`

	// MinGasPrice sets the minimum gas price in wei. Must be > 0.
	MinGasPrice uint64 `json:"minGasPrice"`

	// TimeToDouble specifies the time (in seconds) for the gas price to double
	// when operating at maximum capacity.
	TimeToDouble uint64 `json:"timeToDouble"`
}

var (
	ErrMinGasPriceTooLow      = errors.New("minGasPrice must be greater than 0")
	ErrTargetGasMustBeZero    = errors.New("targetGas must be 0 when validatorTargetGas is true")
	ErrTargetGasTooLowACP224  = fmt.Errorf("targetGas must be at least %d", MinTargetGasACP224)
	ErrTimeToDoubleTooLow     = errors.New("timeToDouble must be greater than 0")
	ErrTimeToDoubleMustBeZero = errors.New("timeToDouble must be 0 when staticPricing is true")
)

// Verify checks fields of this ACP224FeeConfig to ensure a valid configuration is provided.
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

// Equal checks if given [other] is same with this ACP224FeeConfig.
func (a *ACP224FeeConfig) Equal(other *ACP224FeeConfig) bool {
	if other == nil {
		return false
	}

	return *a == *other
}
