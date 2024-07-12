// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/avm/txs/fees"
)

// Struct collecting all the foundational parameters of the AVM
type Config struct {
	fees.StaticConfig

	// Time of the E network upgrade
	EUpgradeTime time.Time
}

func (c *Config) IsEActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.EUpgradeTime)
}
