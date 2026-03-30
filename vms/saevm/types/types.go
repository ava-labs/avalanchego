// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package types defines common types that are used throughout the SAE
// repository which otherwise do not have a clear package to originate from.
//
// Imports of other packages should be extremely limited to avoid circular
// dependencies.
package types

import (
	"errors"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

type (
	// A BlockSource returns a Block that matches both a hash and number, and a
	// boolean indicating if such a block was found.
	BlockSource func(hash common.Hash, number uint64) (*types.Block, bool)
	// A HeaderSource is equivalent to a [BlockSource] except that it only
	// returns the block header.
	HeaderSource func(hash common.Hash, number uint64) (*types.Header, bool)
)

// ExecutionResults provides type safety for a [database.HeightIndex], to be
// used for persistence of SAE-specific execution results, avoiding possible
// collision with `rawdb` keys.
type ExecutionResults struct {
	database.HeightIndex
}

// GasPriceConfig contains gas-related parameters that can be configured via hooks.
type GasPriceConfig struct {
	// TargetToExcessScaling is the ratio between the gas target and the
	// reciprocal of the excess coefficient used in price calculation
	// (K variable in ACP-176, where K = TargetToExcessScaling * T).
	// MUST be non-zero.
	TargetToExcessScaling gas.Gas
	// MinPrice is the minimum gas price / base fee (M parameter in ACP-176).
	// MUST be non-zero.
	MinPrice gas.Price
	// StaticPricing is a flag indicating whether the gas price should be static
	// at the minimum price.
	StaticPricing bool
}

var (
	errTargetToExcessScalingZero = errors.New("targetToExcessScaling must be non-zero")
	errMinPriceZero              = errors.New("minPrice must be non-zero")
)

// Validate checks that the GasPriceConfig fields are valid.
func (c *GasPriceConfig) Validate() error {
	if c.TargetToExcessScaling == 0 {
		return errTargetToExcessScalingZero
	}
	// TODO (ceyonur): Decide whether we want to allow zero min price exclusive for static pricing,
	// to support fee-less networks.
	// https://github.com/ava-labs/strevm/issues/266
	if c.MinPrice == 0 {
		return errMinPriceZero
	}
	return nil
}
