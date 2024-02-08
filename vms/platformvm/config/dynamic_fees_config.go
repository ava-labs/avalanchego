// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

// Dynamic fees configs become relevant with dynamic fees introduction in E-fork
// We cannot easily include then in Config since they do not come from genesis
// They don't feel like an execution config either, since we need a fork upgrade
// to update them (testing is a different story).
// I am setting them in a separate config object, but will access it via Config
// so to have fork control over which dynamic fees is picked

// EUpgradeDynamicFeesConfig to be tuned TODO ABENEGIA
var (
	EUpgradeDynamicFeesConfig = DynamicFeesConfig{
		UnitFees: commonfees.Dimensions{
			1,
			2,
			3,
			4,
		},

		BlockUnitsCap: commonfees.Max,
	}

	PreEUpgradeDynamicFeesConfig = DynamicFeesConfig{
		UnitFees:      commonfees.Empty,
		BlockUnitsCap: commonfees.Max,
	}
)

type DynamicFeesConfig struct {
	// UnitFees contains, per each fee dimension, the
	// unit fees valid as soon as fork introducing dynamic fees
	// activates. Unit fees will be then updated by the dynamic fees algo.
	UnitFees commonfees.Dimensions

	// BlockUnitsCap contains, per each fee dimension, the
	// maximal complexity a valid P-chain block can host
	BlockUnitsCap commonfees.Dimensions
}
