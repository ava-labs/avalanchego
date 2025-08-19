// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/cortina"
)

var (
	errInvalidExtraDataGasUsed = errors.New("invalid extra data gas used")
	errInvalidGasUsed          = errors.New("invalid gas used")
	errInvalidGasLimit         = errors.New("invalid gas limit")
)

// GasLimit takes the previous header and the timestamp of its child block and
// calculates the gas limit for the child block.
func GasLimit(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (uint64, error) {
	switch {
	case config.IsFortuna(timestamp):
		state, err := feeStateBeforeBlock(config, parent, timestamp)
		if err != nil {
			return 0, fmt.Errorf("calculating initial fee state: %w", err)
		}
		// The gas limit is set to the maximum capacity, rather than the current
		// capacity, to minimize the differences with upstream geth. During
		// block building and gas usage calculations, the gas limit is checked
		// against the current capacity.
		return uint64(state.MaxCapacity()), nil
	case config.IsCortina(timestamp):
		return cortina.GasLimit, nil
	case config.IsApricotPhase1(timestamp):
		return ap1.GasLimit, nil
	default:
		// The gas limit prior Apricot Phase 1 started at the genesis value and
		// migrated towards the [ap1.GasLimit] following the `core.CalcGasLimit`
		// updates. However, since all chains have activated Apricot Phase 1,
		// this code is not used in production. To avoid a dependency on the
		// `core` package, this code is modified to just return the parent gas
		// limit; which was valid to do prior to Apricot Phase 1.
		return parent.GasLimit, nil
	}
}

// VerifyGasUsed verifies that the gas used is less than or equal to the gas
// limit.
func VerifyGasUsed(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	gasUsed := header.GasUsed
	extDataGasUsed := customtypes.GetHeaderExtra(header).ExtDataGasUsed
	if config.IsFortuna(header.Time) && extDataGasUsed != nil {
		if !extDataGasUsed.IsUint64() {
			return fmt.Errorf("%w: %d is not a uint64",
				errInvalidExtraDataGasUsed,
				extDataGasUsed,
			)
		}
		var err error
		gasUsed, err = math.Add(gasUsed, extDataGasUsed.Uint64())
		if err != nil {
			return fmt.Errorf("%w while calculating gas used", err)
		}
	}

	capacity, err := GasCapacity(config, parent, header.Time)
	if err != nil {
		return fmt.Errorf("calculating gas capacity: %w", err)
	}
	if gasUsed > capacity {
		return fmt.Errorf("%w: have %d, capacity %d",
			errInvalidGasUsed,
			gasUsed,
			capacity,
		)
	}
	return nil
}

// VerifyGasLimit verifies that the gas limit for the header is valid.
func VerifyGasLimit(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsFortuna(header.Time):
		state, err := feeStateBeforeBlock(config, parent, header.Time)
		if err != nil {
			return fmt.Errorf("calculating initial fee state: %w", err)
		}
		maxCapacity := state.MaxCapacity()
		if header.GasLimit != uint64(maxCapacity) {
			return fmt.Errorf("%w: have %d, want %d",
				errInvalidGasLimit,
				header.GasLimit,
				maxCapacity,
			)
		}
	case config.IsCortina(header.Time):
		if header.GasLimit != cortina.GasLimit {
			return fmt.Errorf("%w: expected to be %d in Cortina, but found %d",
				errInvalidGasLimit,
				cortina.GasLimit,
				header.GasLimit,
			)
		}
	case config.IsApricotPhase1(header.Time):
		if header.GasLimit != ap1.GasLimit {
			return fmt.Errorf("%w: expected to be %d in ApricotPhase1, but found %d",
				errInvalidGasLimit,
				ap1.GasLimit,
				header.GasLimit,
			)
		}
	default:
		if header.GasLimit < ap0.MinGasLimit || header.GasLimit > ap0.MaxGasLimit {
			return fmt.Errorf("%w: %d not in range [%d, %d]",
				errInvalidGasLimit,
				header.GasLimit,
				ap0.MinGasLimit,
				ap0.MaxGasLimit,
			)
		}

		// Verify that the gas limit remains within allowed bounds
		diff := math.AbsDiff(parent.GasLimit, header.GasLimit)
		limit := parent.GasLimit / ap0.GasLimitBoundDivisor
		if diff >= limit {
			return fmt.Errorf("%w: have %d, want %d += %d",
				errInvalidGasLimit,
				header.GasLimit,
				parent.GasLimit,
				limit,
			)
		}
	}
	return nil
}

// GasCapacity takes the previous header and the timestamp of its child block
// and calculates the available gas that can be consumed in the child block.
func GasCapacity(
	config *extras.ChainConfig,
	parent *types.Header,
	timestamp uint64,
) (uint64, error) {
	// Prior to the F upgrade, the gas capacity is equal to the gas limit.
	if !config.IsFortuna(timestamp) {
		return GasLimit(config, parent, timestamp)
	}

	state, err := feeStateBeforeBlock(config, parent, timestamp)
	if err != nil {
		return 0, fmt.Errorf("calculating initial fee state: %w", err)
	}
	return uint64(state.Gas.Capacity), nil
}

// RemainingAtomicGasCapacity returns the maximum amount ExtDataGasUsed could be
// on `header` while still being valid based on the initial capacity and
// consumed gas.
func RemainingAtomicGasCapacity(
	config *extras.ChainConfig,
	parent *types.Header,
	header *types.Header,
) (uint64, error) {
	// Prior to the F upgrade, the atomic gas limit was a constant independent
	// of the evm gas used.
	if !config.IsFortuna(header.Time) {
		return ap5.AtomicGasLimit, nil
	}

	state, err := feeStateBeforeBlock(config, parent, header.Time)
	if err != nil {
		return 0, fmt.Errorf("calculating initial fee state: %w", err)
	}
	if err := state.ConsumeGas(header.GasUsed, nil); err != nil {
		return 0, fmt.Errorf("%w while calculating available gas", err)
	}
	return uint64(state.Gas.Capacity), nil
}
