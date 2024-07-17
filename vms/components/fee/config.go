// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

type Config struct {
	// Weights to merge fees dimensions complexities into a single gas value
	Weights Dimensions `json:"weights"`
	// Target amount of gas the chain should consume per second to keep the fees
	// stable.
	TargetGasPerSecond Gas `json:"targetGasPerSecond"`
	// Maximum amount of gas the chain is allowed to consume per second.
	MaxGasPerSecond Gas `json:"maxGasPerSecond"`
	// Maximum amount of gas the chain is allowed to store for future use.
	MaxGasCapacity Gas `json:"maxGasCapacity"`
	// Minimum price in nAVAX per unit of gas.
	MinGasPrice GasPrice `json:"minGasPrice"`
	// Constant used to
	GasConversionConstant Gas `json:"gasConversionConstant"`
}
