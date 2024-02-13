// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

// Dynamic fees configs become relevant with dynamic fees introduction in E-fork
// We cannot easily include then in Config since they do not come from genesis
// They don't feel like an execution config either, since we need a fork upgrade
// to update them (testing is a different story).
// I am setting them in a separate config object, but will access it via Config
// so to have fork control over which dynamic fees is picked

func init() {
	if err := EUpgradeDynamicFeesConfig.validate(); err != nil {
		panic(err)
	}
}

// EUpgradeDynamicFeesConfig to be tuned TODO ABENEGIA
var (
	EUpgradeDynamicFeesConfig = DynamicFeesConfig{
		InitialUnitFees: commonfees.Dimensions{
			1 * units.NanoAvax,
			2 * units.NanoAvax,
			3 * units.NanoAvax,
			4 * units.NanoAvax,
		},

		MinUnitFees: commonfees.Dimensions{},

		FeesChangeDenominator: commonfees.Dimensions{
			1 * units.NanoAvax,
			1 * units.NanoAvax,
			1 * units.NanoAvax,
			1 * units.NanoAvax,
		},

		BlockUnitsCap: commonfees.Dimensions{
			math.MaxUint64,
			math.MaxUint64,
			math.MaxUint64,
			math.MaxUint64,
		},

		BlockUnitsTarget: commonfees.Dimensions{
			1,
			1,
			1,
			1,
		},
	}

	PreEUpgradeDynamicFeesConfig = DynamicFeesConfig{
		InitialUnitFees: commonfees.Empty,
		BlockUnitsCap:   commonfees.Max,
	}
)

type DynamicFeesConfig struct {
	// InitialUnitFees contains, per each fee dimension, the
	// unit fees valid as soon as fork introducing dynamic fees
	// activates. Unit fees will be then updated by the dynamic fees algo.
	InitialUnitFees commonfees.Dimensions

	// MinUnitFees contains, per each fee dimension, the
	// minimal unit fees enforced by the dynamic fees algo.
	MinUnitFees commonfees.Dimensions

	// FeesChangeDenominator contains, per each fee dimension, the
	// minimal unit fees change
	FeesChangeDenominator commonfees.Dimensions

	// BlockUnitsCap contains, per each fee dimension, the
	// maximal complexity a valid P-chain block can host
	BlockUnitsCap commonfees.Dimensions

	// BlockUnitsTarget contains, per each fee dimension, the
	// preferred block complexity that the dynamic fee algo
	// strive to converge to
	BlockUnitsTarget commonfees.Dimensions
}

func (c *DynamicFeesConfig) validate() error {
	for i := commonfees.Dimension(0); i < commonfees.FeeDimensions; i++ {
		if c.InitialUnitFees[i] < c.MinUnitFees[i] {
			return fmt.Errorf("dimension %d, initial unit fee %d smaller than minimal unit fee %d",
				i,
				c.InitialUnitFees[i],
				c.MinUnitFees[i],
			)
		}

		if c.FeesChangeDenominator[i] == 0 {
			return fmt.Errorf("dimension %d, fees change denominator set to zero", i)
		}

		if c.BlockUnitsTarget[i] > c.BlockUnitsCap[i] {
			return fmt.Errorf("dimension %d, block target units %d larger than block units cap %d",
				i,
				c.BlockUnitsTarget[i],
				c.BlockUnitsCap[i],
			)
		}

		if c.BlockUnitsTarget[i] == 0 {
			return fmt.Errorf("dimension %d, block target units set to zero", i)
		}
	}

	return nil
}
