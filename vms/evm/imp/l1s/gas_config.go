// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"math"

	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/utils/math/intmath"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
)

// genesisGasConfig returns the gas config in effect at genesisTime. It matches
// the state seeded by [gaspricemanager.Configure].
func genesisGasConfig(cfg *extras.ChainConfig, genesisTime uint64) commontype.GasPriceConfig {
	if cfg.IsPrecompileEnabled(gaspricemanager.ContractAddress, genesisTime) {
		precompileConfig := cfg.GetActivePrecompileConfig(gaspricemanager.ContractAddress, genesisTime)
		if c, ok := precompileConfig.(*gaspricemanager.Config); ok && c.InitialGasPriceConfig != nil {
			return *c.InitialGasPriceConfig
		}
	}
	return commontype.DefaultGasPriceConfig()
}

// readGasConfig returns the gas config in effect under rules, read from state.
func readGasConfig(rules *extras.Rules, state libevm.StateReader) commontype.GasPriceConfig {
	if !rules.IsPrecompileEnabled(gaspricemanager.ContractAddress) {
		return commontype.DefaultGasPriceConfig()
	}
	return gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
}

// setGasConfig writes c into he, including TargetExponent if c pins the target.
func setGasConfig(he *customtypes.HeaderExtra, c commontype.GasPriceConfig) {
	if !c.ValidatorTargetGas {
		he.TargetExponent = new(dynamic.DesiredTargetExponent(gas.Gas(c.TargetGas)))
	}
	he.TargetToExcessScaling = new(scalingFromTimeToDouble(c.TimeToDouble))
	he.MinPrice = new(c.MinGasPrice)
}

// scalingFromTimeToDouble converts ACP-224's TimeToDouble into ACP-176's
// TargetToExcessScaling.
func scalingFromTimeToDouble(ttd uint64) uint64 {
	const defaultTimeToDouble = 60 // seconds
	scaling, _, err := intmath.MulDiv(ttd, gastime.DefaultTargetToExcessScaling, defaultTimeToDouble)
	if err != nil {
		return math.MaxUint64
	}
	return scaling
}
