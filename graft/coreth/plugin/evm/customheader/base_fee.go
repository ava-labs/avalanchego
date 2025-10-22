// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
)

// BaseFee takes the previous header and the timestamp of its child block and
// calculates the expected base fee for the child block.
//
// Prior to AP3, the returned base fee will be nil.
func BaseFee(
	config *extras.ChainConfig,
	parent *types.Header,
	timeMS uint64,
) (*big.Int, error) {
	timestamp := timeMS / 1000
	switch {
	case config.IsFortuna(timestamp):
		state, err := feeStateBeforeBlock(config, parent, timeMS)
		if err != nil {
			return nil, fmt.Errorf("calculating initial fee state: %w", err)
		}
		price := state.GasPrice()
		return new(big.Int).SetUint64(uint64(price)), nil
	case config.IsApricotPhase3(timestamp):
		return baseFeeFromWindow(config, parent, timestamp)
	default:
		// Prior to AP3 the expected base fee is nil.
		return nil, nil
	}
}

// EstimateNextBaseFee attempts to estimate the base fee of a block built at
// `timestamp` on top of `parent`.
//
// If timestamp is before parent.Time or the AP3 activation time, then timestamp
// is set to the maximum of parent.Time and the AP3 activation time.
//
// Warning: This function should only be used in estimation and should not be
// used when calculating the canonical base fee for a block.
func EstimateNextBaseFee(
	config *extras.ChainConfig,
	parent *types.Header,
	timeMS uint64,
) (*big.Int, error) {
	parentMS := customtypes.HeaderTimeMilliseconds(parent)
	timeMS = max(timeMS, parentMS)
	return BaseFee(config, parent, timeMS)
}
