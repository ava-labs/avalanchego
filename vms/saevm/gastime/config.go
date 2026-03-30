// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

//nolint:revive // struct-tag: canoto allows unexported fields
type config struct {
	targetToExcessScaling gas.Gas   `canoto:"uint,1"`
	minPrice              gas.Price `canoto:"uint,2"`
	staticPricing         bool      `canoto:"bool,3"`

	canotoData canotoData_config
}

var errInvalidGasPriceConfig = errors.New("invalid gas price config")

// newConfig builds and validates an internal config from [saetypes.GasPriceConfig].
func newConfig(from saetypes.GasPriceConfig) (config, error) {
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
