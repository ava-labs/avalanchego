// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP5 defines constants used after the Apricot Phase 5 upgrade.
package ap5

const (
	// BlockGasCostStep is the rate at which the block gas cost changes per
	// second.
	//
	// This value modifies the previously used `ap4.BlockGasCostStep`.
	BlockGasCostStep = 200_000

	// TargetGas is the target amount of gas to be included in the window. The
	// target amount of gas per second equals [TargetGas] / `ap3.WindowLen`.
	//
	// This value modifies the previously used `ap3.TargetGas`.
	TargetGas = 15_000_000

	// BaseFeeChangeDenominator is the denominator used to smoothen base fee
	// changes.
	//
	// This value modifies the previously used `ap3.BaseFeeChangeDenominator`.
	BaseFeeChangeDenominator = 36

	// AtomicGasLimit specifies the maximum amount of gas that can be consumed
	// by the atomic transactions included in a block.
	//
	// Prior to Apricot Phase 5, a block included a single atomic transaction.
	// As of Apricot Phase 5, each block can include a set of atomic
	// transactions where the cumulative atomic gas consumed is capped by the
	// atomic gas limit, similar to the block gas limit.
	AtomicGasLimit = 100_000

	// AtomicTxIntrinsicGas is the base amount of gas to charge per atomic
	// transaction. There are additional gas costs that can be charged per
	// transaction.
	AtomicTxIntrinsicGas = 10_000
)
