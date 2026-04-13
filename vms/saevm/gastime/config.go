// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	"github.com/ava-labs/avalanchego/vms/saevm/hook"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

type config struct {
	targetToExcessScaling gas.Gas   `canoto:"uint,1"`
	minPrice              gas.Price `canoto:"uint,2"`
	staticPricing         bool      `canoto:"bool,3"`

	canotoData canotoData_config
}

var errInvalidGasPriceConfig = errors.New("invalid gas price config")

// newConfig builds and validates an internal config from [hook.GasPriceConfig].
func newConfig(from hook.GasPriceConfig) (config, error) {
	if err := from.Validate(); err != nil {
		return config{}, fmt.Errorf("%w: %w", errInvalidGasPriceConfig, err)
	}
	c := config{
		targetToExcessScaling: from.TargetToExcessScaling,
		minPrice:              from.MinPrice,
		staticPricing:         from.StaticPricing,
	}
	return c, nil
}

// equal returns true if the logical fields of c and other are equal.
// It ignores canoto internal fields.
func (c config) equals(other config) bool {
	return c.targetToExcessScaling == other.targetToExcessScaling &&
		c.minPrice == other.minPrice &&
		c.staticPricing == other.staticPricing
}
