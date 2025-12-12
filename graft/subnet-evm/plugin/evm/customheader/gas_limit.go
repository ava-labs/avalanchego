// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"

	ethparams "github.com/ava-labs/libevm/params"
)

var (
	errInvalidGasUsed  = errors.New("invalid gas used")
	errInvalidGasLimit = errors.New("invalid gas limit")
)

type CalculateGasLimitFunc func(parentGasUsed, parentGasLimit, gasFloor, gasCeil uint64) uint64

// GasLimit takes the previous header and the timestamp of its child block and
// calculates the gas limit for the child block.
func GasLimit(
	config *extras.ChainConfig,
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timeMS uint64,
) (uint64, error) {
	timestamp := timeMS / 1000
	switch {
	case config.IsSubnetEVM(timestamp):
		return feeConfig.GasLimit.Uint64(), nil
	default:
		// since all chains have activated Subnet-EVM,
		// this code is not used in production. To avoid a dependency on the
		// `core` package, this code is modified to just return the parent gas
		// limit; which was valid to do prior to Subnet-EVM.
		return parent.GasLimit, nil
	}
}

// VerifyGasUsed verifies that the gas used is less than or equal to the gas
// limit.
func VerifyGasUsed(
	config *extras.ChainConfig,
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	header *types.Header,
) error {
	gasUsed := header.GasUsed
	timeMS := customtypes.HeaderTimeMilliseconds(header)
	capacity, err := GasCapacity(config, feeConfig, parent, timeMS)
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
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	header *types.Header,
) error {
	switch {
	case config.IsSubnetEVM(header.Time):
		expectedGasLimit := feeConfig.GasLimit.Uint64()
		if header.GasLimit != expectedGasLimit {
			return fmt.Errorf("%w: expected to be %d in Subnet-EVM, but found %d",
				errInvalidGasLimit,
				expectedGasLimit,
				header.GasLimit,
			)
		}
	default:
		if header.GasLimit < ethparams.MinGasLimit || header.GasLimit > ethparams.MaxGasLimit {
			return fmt.Errorf("%w: %d not in range [%d, %d]",
				errInvalidGasLimit,
				header.GasLimit,
				ethparams.MinGasLimit,
				ethparams.MaxGasLimit,
			)
		}

		// Verify that the gas limit remains within allowed bounds
		diff := math.AbsDiff(parent.GasLimit, header.GasLimit)
		limit := parent.GasLimit / ethparams.GasLimitBoundDivisor
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
	feeConfig commontype.FeeConfig,
	parent *types.Header,
	timeMS uint64,
) (uint64, error) {
	return GasLimit(config, feeConfig, parent, timeMS)
}
