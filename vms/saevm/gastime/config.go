// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// GasPriceConfig contains gas-related parameters that can be configured via hooks.
type GasPriceConfig struct {
	// TargetToExcessScaling is the ratio between the gas target and the
	// reciprocal of the excess coefficient used in price calculation
	// (K variable in ACP-176, where K = TargetToExcessScaling * T).
	// MUST be non-zero.
	TargetToExcessScaling gas.Gas `canoto:"uint,1"`
	// MinPrice is the minimum gas price / base fee (M parameter in ACP-176).
	// MUST be non-zero.
	MinPrice gas.Price `canoto:"uint,2"`
	// StaticPricing is a flag indicating whether the gas price should be static
	// at the minimum price.
	StaticPricing bool `canoto:"bool,3"`

	canotoData canotoData_GasPriceConfig
}

var (
	errTargetToExcessScalingZero = errors.New("targetToExcessScaling must be non-zero")
	errMinPriceZero              = errors.New("minPrice must be non-zero")
)

// Validate checks that the GasPriceConfig fields are valid.
func (c *GasPriceConfig) Validate() error {
	if c.TargetToExcessScaling == 0 {
		return errTargetToExcessScalingZero
	}
	// TODO (ceyonur): Decide whether we want to allow zero min price exclusive for static pricing,
	// to support fee-less networks.
	// https://github.com/ava-labs/strevm/issues/266
	if c.MinPrice == 0 {
		return errMinPriceZero
	}
	return nil
}

// equal returns true if the logical fields of c and other are equal.
// It ignores canoto internal fields.
func (c GasPriceConfig) equals(other GasPriceConfig) bool {
	return c.TargetToExcessScaling == other.TargetToExcessScaling &&
		c.MinPrice == other.MinPrice &&
		c.StaticPricing == other.StaticPricing
}
