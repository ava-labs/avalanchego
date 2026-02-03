// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/graft/evm/utils"
)

// setDefaults applies default values to fields that are still zero-valued.
func (c *Config) setDefaults() {
	// Apply base config defaults first
	c.BaseConfig.SetDefaults()

	// C-Chain specific defaults
	if c.PriceOptionSlowFeePercentage == 0 {
		c.PriceOptionSlowFeePercentage = 95
	}
	if c.PriceOptionFastFeePercentage == 0 {
		c.PriceOptionFastFeePercentage = 105
	}
	if c.PriceOptionMaxTip == 0 {
		c.PriceOptionMaxTip = 20 * utils.GWei
	}
}
