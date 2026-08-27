// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
)

// A headerGasConfig is the effective ACP-224 gas configuration carried by a
// subnet-evm SAE header (the customtypes `GasConfig*` field group). It is the
// projection of gaspricemanager precompile storage in the settled state,
// stamped by [builder.FinalizeHeader] and read back by
// [hooks.GasConfigAfter], making recovery and rebuilds self-contained at the
// header level.
type headerGasConfig struct {
	// ValidatorTargetGas selects the gas-target authority: when true, the
	// header's ACP-176 `TargetExcess` vote remains the source of truth;
	// when false, TargetGas pins the target.
	ValidatorTargetGas bool
	TargetGas          gas.Gas
	GasPriceConfig     gastime.GasPriceConfig
}

// effective returns the gas target and price config represented by the group.
// Validity is the producer's responsibility: [builder.FinalizeHeader]
// only stamps configs whose source passed [commontype.GasPriceConfig.Verify],
// so consumer-side re-validation here would be redundant.
func (c *headerGasConfig) effective(headerTarget gas.Gas) (gas.Gas, gastime.GasPriceConfig) {
	if c.ValidatorTargetGas {
		return headerTarget, c.GasPriceConfig
	}
	return c.TargetGas, c.GasPriceConfig
}

// gasConfigFromStored projects gaspricemanager storage into the derived
// values carried by the header.
func gasConfigFromStored(stored commontype.GasPriceConfig) headerGasConfig {
	return headerGasConfig{
		ValidatorTargetGas: stored.ValidatorTargetGas,
		TargetGas:          gas.Gas(stored.TargetGas),
		GasPriceConfig: gastime.GasPriceConfig{
			TargetToExcessScaling: scalingFromTimeToDouble(stored.TimeToDouble),
			MinPrice:              gas.Price(stored.MinGasPrice),
			StaticPricing:         stored.StaticPricing,
		},
	}
}

// stampGasConfig writes the group into `he`. All five fields are always set
// together; [readGasConfig] treats anything less as absent.
func stampGasConfig(he *customtypes.HeaderExtra, c headerGasConfig) {
	he.GasConfigValidatorTargetGas = boolToUint64Ptr(c.ValidatorTargetGas)
	he.GasConfigTargetGas = (*uint64)(&c.TargetGas)
	he.GasConfigTargetToExcessScaling = (*uint64)(&c.GasPriceConfig.TargetToExcessScaling)
	he.GasConfigMinGasPrice = (*uint64)(&c.GasPriceConfig.MinPrice)
	he.GasConfigStaticPricing = boolToUint64Ptr(c.GasPriceConfig.StaticPricing)
}

// readGasConfig recovers the group stamped by [stampGasConfig], reporting
// `false` when the header does not carry a complete group (gaspricemanager
// not enabled at the settled timestamp, or a pre-SAE header). A maliciously
// crafted header CAN carry present-but-zero fields (RLP decodes empty
// optional items as pointers to zero); such headers are rejected by the
// rebuild-hash-equality check in block verification, never by this reader.
func readGasConfig(he *customtypes.HeaderExtra) (headerGasConfig, bool) {
	if he.GasConfigValidatorTargetGas == nil ||
		he.GasConfigTargetGas == nil ||
		he.GasConfigTargetToExcessScaling == nil ||
		he.GasConfigMinGasPrice == nil ||
		he.GasConfigStaticPricing == nil {
		return headerGasConfig{}, false
	}
	return headerGasConfig{
		ValidatorTargetGas: *he.GasConfigValidatorTargetGas != 0,
		TargetGas:          gas.Gas(*he.GasConfigTargetGas),
		GasPriceConfig: gastime.GasPriceConfig{
			TargetToExcessScaling: gas.Gas(*he.GasConfigTargetToExcessScaling),
			MinPrice:              gas.Price(*he.GasConfigMinGasPrice),
			StaticPricing:         *he.GasConfigStaticPricing != 0,
		},
	}, true
}

func boolToUint64Ptr(b bool) *uint64 {
	var v uint64
	if b {
		v = 1
	}
	return &v
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
	return gas.Gas(math.Round(float64(ttd) / math.Ln2))
}
