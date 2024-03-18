// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

type DynamicFeesConfig struct {
	// UnitFees contains, per each fee dimension, the
	// unit fees valid as soon as fork introducing dynamic fees
	// activates. Unit fees are not currently updated by any dynamic fees algo.
	UnitFees Dimensions `json:"initial-unit-fees"`

	// BlockUnitsCap contains, per each fee dimension, the
	// maximal complexity a valid block can host
	BlockUnitsCap Dimensions `json:"block-unit-caps"`
}
