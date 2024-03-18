// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"github.com/ava-labs/avalanchego/utils/units"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

// EUpgradeDynamicFeesConfig to be tuned TODO ABENEGIA
var (
	EUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		UnitFees: commonfees.Dimensions{
			1 * units.NanoAvax,
			2 * units.NanoAvax,
			3 * units.NanoAvax,
			4 * units.NanoAvax,
		},

		BlockUnitsCap: commonfees.Max,
	}

	PreEUpgradeDynamicFeesConfig = commonfees.DynamicFeesConfig{
		UnitFees:      commonfees.Empty,
		BlockUnitsCap: commonfees.Max,
	}
)
