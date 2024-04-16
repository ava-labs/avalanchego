// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import "time"

// Struct collecting all the foundational parameters of the AVM
type Config struct {
	// Fee that is burned by every non-asset creating transaction
	TxFee uint64

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64

	// Time of the E network upgrade
	EUpgradeTime time.Time
}

func (c *Config) IsEActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.EUpgradeTime)
}
