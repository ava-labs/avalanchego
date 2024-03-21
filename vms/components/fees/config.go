// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

type DynamicFeesConfig struct {
	// FeeRate contains, per each fee dimension, the fee rate,
	// i.e. the fee per unit of complexity. Fee rates are
	// valid as soon as fork introducing dynamic fees activates.
	FeeRate Dimensions `json:"fee-rate"`

	// BlockMaxComplexity contains, per each fee dimension, the
	// maximal complexity a valid block can host.
	BlockMaxComplexity Dimensions `json:"block-max-complexity"`
}
