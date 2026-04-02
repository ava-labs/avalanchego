// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The gas package implements dynamic gas pricing specified in ACP-103:
// https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/103-dynamic-fees
package gas

type Config struct {
	// Weights to merge fee dimensions into a single gas value.
	Weights Dimensions `json:"weights"`
	// Maximum amount of gas the chain is allowed to store for future use.
	MaxCapacity Gas `json:"maxCapacity"`
	// Maximum amount of gas the chain is allowed to consume per second.
	MaxPerSecond Gas `json:"maxPerSecond"`
	// Target amount of gas the chain should consume per second to keep the fees
	// stable.
	TargetPerSecond Gas `json:"targetPerSecond"`
	// Minimum price per unit of gas.
	MinPrice Price `json:"minPrice"`
	// Constant used to convert excess gas to a gas price.
	ExcessConversionConstant Gas `json:"excessConversionConstant"`
}
