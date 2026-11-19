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

// defaultTimeToDouble is the ACP-224 TimeToDouble, in seconds, that ACP-176
// approximates as a TargetToExcessScaling of 87.
const defaultTimeToDouble = 60

// setGasConfig writes the gaspricemanager config c into he, overwriting
// he.TargetExponent if c pins the target. ACP-224 requires TimeToDouble to be 0
// exactly for static pricing, so static pricing is written as a zero
// TargetToExcessScaling.
func setGasConfig(he *customtypes.HeaderExtra, c commontype.GasPriceConfig) {
	if !c.ValidatorTargetGas {
		he.TargetExponent = new(dynamic.DesiredTargetExponent(gas.Gas(c.TargetGas)))
	}
	he.TargetToExcessScaling = new(scalingFromTimeToDouble(c.TimeToDouble))
	he.MinPrice = new(c.MinGasPrice)
}

// scalingFromTimeToDouble converts ACP-224's TimeToDouble, in seconds, into
// the ACP-176 TargetToExcessScaling. It scales ACP-176's approximation of 87
// for a 60s doubling time linearly.
func scalingFromTimeToDouble(ttd uint64) uint64 {
	scaling, _, err := intmath.MulDiv(ttd, gastime.DefaultTargetToExcessScaling, defaultTimeToDouble)
	if err != nil {
		return math.MaxUint64
	}
	return scaling
}
