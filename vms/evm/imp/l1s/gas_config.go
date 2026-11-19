// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils/math/intmath"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
)

// gasConfigFromStored projects gaspricemanager storage into the derived
// values carried by the header.
func gasConfigFromStored(stored commontype.GasPriceConfig) gastime.GasPriceConfig {
	return gastime.GasPriceConfig{
		TargetToExcessScaling: scalingFromTimeToDouble(stored.TimeToDouble),
		MinPrice:              gas.Price(stored.MinGasPrice),
		StaticPricing:         stored.StaticPricing,
	}
}

// stampStoredGasConfig writes the target and gas-config group of stored
// gaspricemanager storage into he.
func stampStoredGasConfig(he *customtypes.HeaderExtra, stored commontype.GasPriceConfig) {
	if !stored.ValidatorTargetGas {
		he.TargetExcess = new(gas.Gas(dynamic.DesiredTargetExponent(gas.Gas(stored.TargetGas))))
	}
	stampGasConfig(he, gasConfigFromStored(stored))
}

// stampGasConfig writes c into he, encoding static pricing as a zero
// TargetToExcessScaling.
func stampGasConfig(he *customtypes.HeaderExtra, c gastime.GasPriceConfig) {
	scaling := uint64(c.TargetToExcessScaling)
	if c.StaticPricing {
		scaling = 0
	}
	he.TargetToExcessScaling = &scaling
	he.MinPrice = (*uint64)(&c.MinPrice)
}

// readGasConfig returns the config stamped by [stampGasConfig], or false if he
// has none, which only happens before the first SAE block. Static pricing
// ignores the scaling, but gastime requires a non-zero one, so it reads as
// the default.
func readGasConfig(he *customtypes.HeaderExtra) (gastime.GasPriceConfig, bool) {
	if he.TargetToExcessScaling == nil || he.MinPrice == nil {
		return gastime.GasPriceConfig{}, false
	}
	c := gastime.GasPriceConfig{
		TargetToExcessScaling: gas.Gas(*he.TargetToExcessScaling),
		MinPrice:              gas.Price(*he.MinPrice),
	}
	if c.TargetToExcessScaling == 0 {
		c.TargetToExcessScaling = gastime.DefaultTargetToExcessScaling
		c.StaticPricing = true
	}
	return c, true
}

// scalingFromTimeToDouble converts ACP-224's TimeToDouble, in seconds, into
// the ACP-176 TargetToExcessScaling. It scales ACP-176's approximation of 87
// for a 60s doubling time linearly.
//
// TimeToDouble is 0 for static pricing, which ignores the scaling, but gastime
// requires a non-zero scaling, so 0 maps to the default.
func scalingFromTimeToDouble(ttd uint64) gas.Gas {
	if ttd == 0 {
		return gastime.DefaultTargetToExcessScaling
	}
	const defaultTimeToDouble = 60 // seconds
	scaling, _, err := intmath.MulDiv(ttd, gastime.DefaultTargetToExcessScaling, defaultTimeToDouble)
	if err != nil {
		return math.MaxUint64
	}
	return gas.Gas(scaling)
}
