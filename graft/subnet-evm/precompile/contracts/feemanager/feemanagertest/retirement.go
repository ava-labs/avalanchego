// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package feemanagertest provides shared test fixtures for asserting
// feeManager retirement behavior across both the legacy and SAE
// subnet-evm binaries. Only the canonical scenario table and JSON
// encoders live here; each consumer (VM integration test or in-memory
// unit test) writes its own per-case loop, init/parse step, and
// assertions against the [RetirementCase] fields.
package feemanagertest

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/nativeminter"
	"github.com/ava-labs/avalanchego/utils"
)

// RetirementCase is one parse-time scenario for the feeManager
// retirement at Helicon. Timestamps are absolute; the caller plugs in
// the actual Helicon activation timestamp via [RetirementCases] so the
// pre/at/post-Helicon math lines up with the VM's fork harness.
//
// `GenesisPrecompiles` and `Upgrades` are raw `extras` types so cases
// can mix feeManager and foreign-key entries without an intermediate
// translation layer.
type RetirementCase struct {
	Name string
	// GenesisTimestamp is the chain's genesis-block timestamp.
	GenesisTimestamp uint64
	// GenesisPrecompiles is the `GenesisPrecompiles` map to install
	// (nil for cases that don't seed any).
	GenesisPrecompiles extras.Precompiles
	// Upgrades is the `PrecompileUpgrades` slice to install in
	// `upgradeBytes` (nil for cases that don't seed any).
	Upgrades []extras.PrecompileUpgrade

	// WantErr is the error the parser must return; nil means
	// `Initialize` must succeed.
	WantErr error
	// WantGenesisPrecompiles is the expected post-init
	// `cfg.GenesisPrecompiles` map. Asserted strictly via
	// `require.Equal` only on success paths (`WantErr == nil`). The
	// JSON unmarshal in `vm.Initialize` always produces a non-nil
	// map, so empty cases should use `extras.Precompiles{}`.
	WantGenesisPrecompiles extras.Precompiles
	// WantPrecompileUpgrades is the expected post-init
	// `cfg.PrecompileUpgrades` slice. Asserted strictly via
	// `require.Equal` only on success paths. Cases without input
	// upgrades AND no synthetic-disable injection should use `nil`.
	WantPrecompileUpgrades []extras.PrecompileUpgrade
}

// RetirementCases returns the canonical scenario table for feeManager
// retirement at `helicon`. Covers parse-time validation + genesis
// normalization + synthetic-disable injection across all surviving
// configurations.
func RetirementCases(helicon uint64) []RetirementCase {
	preHelicon := helicon - 5
	postHelicon := helicon + 5

	fmEnable := func(ts uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: feemanager.NewConfig(utils.PointerTo(ts), nil, nil, nil, nil)}
	}
	fmDisable := func(ts uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: feemanager.NewDisableConfig(utils.PointerTo(ts))}
	}
	fmGenesis := func(ts uint64) extras.Precompiles {
		return extras.Precompiles{
			feemanager.ConfigKey: feemanager.NewConfig(utils.PointerTo(ts), nil, nil, nil, nil),
		}
	}
	mintEnable := func(ts uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: nativeminter.NewConfig(utils.PointerTo(ts), nil, nil, nil, nil)}
	}
	mintDisable := func(ts uint64) extras.PrecompileUpgrade {
		return extras.PrecompileUpgrade{Config: nativeminter.NewDisableConfig(utils.PointerTo(ts))}
	}

	return []RetirementCase{
		{
			Name:                   "upgradeBytes_enable_pre_Helicon_allowed",
			GenesisTimestamp:       preHelicon,
			Upgrades:               []extras.PrecompileUpgrade{fmEnable(preHelicon)},
			WantGenesisPrecompiles: extras.Precompiles{},
			// Pre-Helicon enable leaves feeManager active going into
			// Helicon: ForceDisable injects the synthetic Disable.
			WantPrecompileUpgrades: []extras.PrecompileUpgrade{fmEnable(preHelicon), fmDisable(helicon)},
		},
		{
			Name:             "upgradeBytes_enable_at_Helicon_rejected",
			GenesisTimestamp: preHelicon,
			Upgrades:         []extras.PrecompileUpgrade{fmEnable(helicon)},
			WantErr:          feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			Name:             "upgradeBytes_enable_post_Helicon_rejected",
			GenesisTimestamp: preHelicon,
			Upgrades:         []extras.PrecompileUpgrade{fmEnable(postHelicon)},
			WantErr:          feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			Name:                   "upgradeBytes_enable_and_disable_both_pre_Helicon_allowed",
			GenesisTimestamp:       preHelicon - 10,
			Upgrades:               []extras.PrecompileUpgrade{fmEnable(preHelicon - 1), fmDisable(preHelicon)},
			WantGenesisPrecompiles: extras.Precompiles{},
			// Last event before Helicon is Disable: feeManager is not
			// active, no synthetic injection.
			WantPrecompileUpgrades: []extras.PrecompileUpgrade{fmEnable(preHelicon - 1), fmDisable(preHelicon)},
		},
		{
			Name:             "upgradeBytes_disable_at_Helicon_rejected",
			GenesisTimestamp: preHelicon,
			Upgrades:         []extras.PrecompileUpgrade{fmDisable(helicon)},
			WantErr:          feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			Name:             "upgradeBytes_disable_post_Helicon_rejected",
			GenesisTimestamp: preHelicon,
			Upgrades:         []extras.PrecompileUpgrade{fmEnable(preHelicon), fmDisable(postHelicon)},
			WantErr:          feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			Name:                   "genesis_enable_pre_Helicon_preserved_inject_at_helicon",
			GenesisTimestamp:       preHelicon - 10,
			GenesisPrecompiles:     fmGenesis(preHelicon - 5),
			WantGenesisPrecompiles: fmGenesis(preHelicon - 5),
			// Genesis enable pre-Helicon stays active through Helicon:
			// ForceDisable injects the synthetic Disable.
			WantPrecompileUpgrades: []extras.PrecompileUpgrade{fmDisable(helicon)},
		},
		{
			Name:               "genesis_enable_post_Helicon_pre_Helicon_genesis_chain_normalized_away",
			GenesisTimestamp:   preHelicon,
			GenesisPrecompiles: fmGenesis(postHelicon),
			// Normalize drops the entry on a pre-Helicon-genesis chain
			// (genesis is immutable; otherwise Verify would brick it).
			WantGenesisPrecompiles: extras.Precompiles{},
			WantPrecompileUpgrades: nil,
		},
		{
			Name:               "genesis_enable_post_Helicon_post_Helicon_genesis_chain_rejected",
			GenesisTimestamp:   postHelicon,
			GenesisPrecompiles: fmGenesis(postHelicon),
			WantErr:            feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		{
			Name:               "genesis_enable_post_Helicon_and_upgradeBytes_disable_post_Helicon_rejected_on_upgrades",
			GenesisTimestamp:   postHelicon,
			GenesisPrecompiles: fmGenesis(postHelicon),
			Upgrades:           []extras.PrecompileUpgrade{fmDisable(postHelicon)},
			WantErr:            feemanager.ErrFeeManagerEnabledAfterHelicon,
		},
		// --- ForceDisable / synthetic-injection scenarios. ---
		{
			Name:                   "no_feeManager_anywhere_no_op",
			GenesisTimestamp:       preHelicon,
			WantGenesisPrecompiles: extras.Precompiles{},
			WantPrecompileUpgrades: nil,
		},
		{
			Name:                   "feeManager_pre_disabled_before_helicon_no_injection",
			GenesisTimestamp:       preHelicon,
			GenesisPrecompiles:     fmGenesis(0),
			Upgrades:               []extras.PrecompileUpgrade{fmDisable(preHelicon)},
			WantGenesisPrecompiles: fmGenesis(0),
			// Last event before Helicon is the explicit Disable: no
			// synthetic injection.
			WantPrecompileUpgrades: []extras.PrecompileUpgrade{fmDisable(preHelicon)},
		},
		{
			Name:                   "foreign_key_upgrades_straddling_Helicon_synthetic_lands_between",
			GenesisTimestamp:       preHelicon,
			GenesisPrecompiles:     fmGenesis(0),
			Upgrades:               []extras.PrecompileUpgrade{mintEnable(preHelicon), mintDisable(postHelicon)},
			WantGenesisPrecompiles: fmGenesis(0),
			// Foreign-key upgrades preserved on both sides; synthetic
			// Disable lands at `helicon` between them.
			WantPrecompileUpgrades: []extras.PrecompileUpgrade{mintEnable(preHelicon), fmDisable(helicon), mintDisable(postHelicon)},
		},
		// --- Foreign-key genesis preservation. ---
		// Normalize must only ever touch the `feeManager` entry; these
		// cases pin that invariant.
		{
			Name:             "genesis_foreign_key_only_no_feeManager",
			GenesisTimestamp: preHelicon,
			GenesisPrecompiles: extras.Precompiles{
				nativeminter.ConfigKey: nativeminter.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil),
			},
			WantGenesisPrecompiles: extras.Precompiles{
				nativeminter.ConfigKey: nativeminter.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil),
			},
			WantPrecompileUpgrades: nil,
		},
		{
			Name:             "genesis_post_Helicon_feeManager_dropped_foreign_key_preserved",
			GenesisTimestamp: preHelicon,
			GenesisPrecompiles: extras.Precompiles{
				feemanager.ConfigKey:   feemanager.NewConfig(utils.PointerTo(postHelicon), nil, nil, nil, nil),
				nativeminter.ConfigKey: nativeminter.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil),
			},
			// Normalize drops only the feeManager entry; the
			// nativeminter genesis entry survives untouched.
			WantGenesisPrecompiles: extras.Precompiles{
				nativeminter.ConfigKey: nativeminter.NewConfig(utils.PointerTo[uint64](0), nil, nil, nil, nil),
			},
			WantPrecompileUpgrades: nil,
		},
	}
}

// EncodeGenesisJSON builds the `core.Genesis` JSON for `tc` against a
// shallow copy of `base`. Mutating `base` post-call is safe.
func EncodeGenesisJSON(t *testing.T, base *params.ChainConfig, tc RetirementCase) []byte {
	t.Helper()
	chainConfig := params.Copy(base)
	if len(tc.GenesisPrecompiles) > 0 {
		params.GetExtra(&chainConfig).GenesisPrecompiles = tc.GenesisPrecompiles
	}
	g := &core.Genesis{
		Config:     &chainConfig,
		Difficulty: big.NewInt(0),
		GasLimit:   8_000_000,
		Timestamp:  tc.GenesisTimestamp,
		Alloc:      types.GenesisAlloc{},
	}
	out, err := json.Marshal(g)
	require.NoError(t, err)
	return out
}

// EncodeUpgradeBytesJSON builds the upgradeBytes JSON for `tc`, or
// returns nil when the case has no upgrade entries.
func EncodeUpgradeBytesJSON(t *testing.T, tc RetirementCase) []byte {
	t.Helper()
	if len(tc.Upgrades) == 0 {
		return nil
	}
	out, err := json.Marshal(&extras.UpgradeConfig{PrecompileUpgrades: tc.Upgrades})
	require.NoError(t, err)
	return out
}
