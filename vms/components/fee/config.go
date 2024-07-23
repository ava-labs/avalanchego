// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The fee package implements dynamic gas pricing specified in ACP-103:
// https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/103-dynamic-fees
package fee

type Config struct {
	// Weights to merge fee dimensions into a single gas value.
	Weights Dimensions `json:"weights"`
	// Maximum amount of gas the chain is allowed to store for future use.
	MaxGasCapacity Gas `json:"maxGasCapacity"`
	// Maximum amount of gas the chain is allowed to consume per second.
	MaxGasPerSecond Gas `json:"maxGasPerSecond"`
	// Target amount of gas the chain should consume per second to keep the fees
	// stable.
	TargetGasPerSecond Gas `json:"targetGasPerSecond"`
	// Minimum price per unit of gas.
	MinGasPrice GasPrice `json:"minGasPrice"`
	// Constant used to convert excess gas to a gas price.
	ExcessConversionConstant Gas `json:"excessConversionConstant"`
}
