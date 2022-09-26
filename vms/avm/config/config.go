// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"
)

// Struct collecting all foundational parameters of AVM
type Config struct {
	TxFees

	// Time of the Blueberry network upgrade
	BlueberryTime time.Time
}

func (c *Config) IsBlueberryActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.BlueberryTime)
}
