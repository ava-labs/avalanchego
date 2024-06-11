// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

type DynamicFeesConfig struct {
	GasPrice GasPrice `json:"gas-price"`

	// weights to merge fees dimensions complexities into a single gas value
	FeeDimensionWeights Dimensions `json:"fee-dimension-weights"`

	// TODO ABENEGIA: replace with leaky bucket implementation
	TempBlockMaxGas Gas
}
