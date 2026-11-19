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

// stampGasConfig writes the group into `he`. All three fields are always set
// together; [readGasConfig] treats anything less as absent.
func stampGasConfig(he *customtypes.HeaderExtra, c gastime.GasPriceConfig) {
	he.TargetToExcessScaling = (*uint64)(&c.TargetToExcessScaling)
	he.MinPrice = (*uint64)(&c.MinPrice)
	he.StaticPricing = &c.StaticPricing
}

// readGasConfig recovers the group stamped by [stampGasConfig], reporting
// `false` when the header does not carry a complete group, which only happens
// before the first SAE block. A maliciously
// crafted header CAN carry present-but-zero fields (RLP decodes empty
// optional items as pointers to zero); such headers are rejected by the
// rebuild-hash-equality check in block verification, never by this reader.
func readGasConfig(he *customtypes.HeaderExtra) (gastime.GasPriceConfig, bool) {
	if he.TargetToExcessScaling == nil ||
		he.MinPrice == nil ||
		he.StaticPricing == nil {
		return gastime.GasPriceConfig{}, false
	}
	return gastime.GasPriceConfig{
		TargetToExcessScaling: gas.Gas(*he.TargetToExcessScaling),
		MinPrice:              gas.Price(*he.MinPrice),
		StaticPricing:         *he.StaticPricing,
	}, true
}

// scalingFromTimeToDouble converts ACP-224's `TimeToDouble` (seconds) into the
// K/T ratio used by ACP-176 / gastime: K = T * TimeToDouble / ln(2), so
// TargetToExcessScaling = round(TimeToDouble / ln(2)). The default 60s
// round-trips to [gastime.DefaultTargetToExcessScaling].
//
// For `StaticPricing` configs `TimeToDouble` is 0 and unused (gastime zeroes
// excess in that branch instead of scaling), but the gastime invariant
// requires `TargetToExcessScaling != 0`, so we return the default.
func scalingFromTimeToDouble(ttd uint64) gas.Gas {
	if ttd == 0 {
		return gastime.DefaultTargetToExcessScaling
	}

	// This continued-fraction convergent computes round(ttd / ln(2)) exactly
	// throughout the uint64 domain without platform-dependent floating point.
	const (
		inverseLn2Numerator   uint64 = 4_403_748_962_482_230_453
		inverseLn2Denominator uint64 = 3_052_446_177_238_342_414
	)
	quotient, remainder, err := intmath.MulDiv(
		ttd,
		inverseLn2Numerator,
		inverseLn2Denominator,
	)
	if err != nil {
		return math.MaxUint64
	}
	if remainder > inverseLn2Denominator/2 {
		quotient = intmath.BoundedAdd(quotient, 1, uint64(math.MaxUint64))
	}
	return gas.Gas(quotient)
}
