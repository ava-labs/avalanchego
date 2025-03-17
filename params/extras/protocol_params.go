// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

const (
	GasLimitBoundDivisor uint64 = 1024               // The bound divisor of the gas limit, used in update calculations.
	MaxGasLimit          uint64 = 0x7fffffffffffffff // Maximum the gas limit (2^63-1).
	MaximumExtraDataSize uint64 = 64                 // Maximum size extra data may be after Genesis.
	MinGasLimit          uint64 = 5000               // Minimum the gas limit may ever be.
)
