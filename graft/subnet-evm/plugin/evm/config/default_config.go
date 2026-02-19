// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/utils"
)

// setDefaults applies default values to fields that are still zero-valued.
func (c *Config) setDefaults() {
	// Apply base config defaults first
	c.BaseConfig.SetDefaults()

	// Subnet-EVM defaults to state sync disabled (unlike C-Chain which enables at genesis)
	if c.StateSyncEnabled == nil {
		c.StateSyncEnabled = utils.PointerTo(false)
	}

	// Subnet-EVM specific defaults
	if c.DatabaseType == "" {
		c.DatabaseType = pebbledb.Name
	}
	if !c.ValidatorsAPIEnabled {
		c.ValidatorsAPIEnabled = true
	}
}
