// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/fees"
)

// Struct collecting all the foundational parameters of the AVM
type Config struct {
	// Fee that is burned by every non-asset creating transaction
	TxFee uint64

	// Fee that must be burned by every asset creating transaction
	CreateAssetTxFee uint64

	// Post E Fork, the unit fee for each dimension, denominated in Avax
	// As long as fees are multidimensional but not dynamic, [DefaultUnitFees]
	// will be the unit fees
	DefaultUnitFees fees.Dimensions

	// Post E Fork, the max complexity of a block for each dimension
	DefaultBlockMaxConsumedUnits fees.Dimensions

	// Time of the Durango network upgrade
	DurangoTime time.Time

	// Time of the E network upgrade
	EForkTime time.Time
}

func (c *Config) IsEForkActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.EForkTime)
}

func (c *Config) BlockMaxConsumedUnits(timestamp time.Time) fees.Dimensions {
	if !c.IsEForkActivated(timestamp) {
		var res fees.Dimensions
		for i := fees.Dimension(0); i < fees.FeeDimensions; i++ {
			res[i] = math.MaxUint64
		}
		return res
	}
	return c.DefaultBlockMaxConsumedUnits
}
