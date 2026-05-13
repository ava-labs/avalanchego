// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"fmt"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/utils"
)

// ReconcileForHelicon validates and normalizes `cfg` for the legacy
// `feeManager` precompile retirement at Helicon. Two steps:
//
//  1. Reject any `feeManager` upgrade in `cfg.PrecompileUpgrades`
//     (enable OR disable) scheduled at `>= helicon`. Such entries are
//     invalid under subnet-evm-sae and operators must remove them from
//     `upgradeBytes`. Returns an error wrapping
//     [ErrFeeManagerEnabledAfterHelicon] with the offending index for
//     diagnosis.
//  2. Compute the post-normalization `GenesisPrecompiles` map: drop a
//     `feeManager` entry that would activate at or after Helicon on a
//     chain whose genesis pre-dates Helicon, so already-running chains
//     do not brick on [Config.Verify]'s post-Helicon rejection.
//     Genesis is immutable for an already-accepted chain, leaving the
//     operator no recourse, hence this safety valve. The same
//     workaround is intentionally NOT applied to
//     `cfg.PrecompileUpgrades` because operators can edit
//     `upgradeBytes` between binary releases.
//
// `cfg` is not mutated; the returned `genesisPrecompiles` is the
// post-normalization map (the original map when no entry needs
// dropping, a new map otherwise). Callers should assign it back to
// `cfg.GenesisPrecompiles` BEFORE calling [ForceDisableAtHelicon] so
// the injection step sees the post-normalize state.
//
// Caller must pass an actually-scheduled `helicon`
// (see [extras.NetworkUpgrades.ScheduledHeliconTimestamp]).
func ReconcileForHelicon(cfg *extras.ChainConfig, genesisTimestamp, helicon uint64) (map[string]precompileconfig.Config, error) {
	if err := rejectUpgradesAtOrAfter(cfg.PrecompileUpgrades, helicon); err != nil {
		return nil, err
	}
	return normalizeGenesisPrecompile(cfg.GenesisPrecompiles, genesisTimestamp, helicon), nil
}

// ForceDisableAtHelicon returns `cfg.PrecompileUpgrades` augmented
// with a synthetic `feeManager Disable: true` upgrade at `helicon`
// when the precompile would otherwise still be active going into
// Helicon (per `cfg.IsPrecompileEnabled`). When no injection is
// needed, returns `cfg.PrecompileUpgrades` unchanged.
//
// `cfg` is not mutated; the returned slice is the post-injection
// view. Callers should assign it back to `cfg.PrecompileUpgrades`.
//
// Caller MUST run [ReconcileForHelicon] first AND assign its result
// back to `cfg.GenesisPrecompiles`. This function relies on
// `cfg.IsPrecompileEnabled` reading the post-normalize genesis to
// avoid a spurious injection when a stale post-Helicon entry would
// otherwise have been dropped.
func ForceDisableAtHelicon(cfg *extras.ChainConfig, helicon uint64) []extras.PrecompileUpgrade {
	if !cfg.IsPrecompileEnabled(ContractAddress, helicon) {
		return cfg.PrecompileUpgrades
	}
	disable := extras.PrecompileUpgrade{
		Config: NewDisableConfig(utils.PointerTo(helicon)),
	}
	out := insertUpgradeBeforeFirstAfter(cfg.PrecompileUpgrades, disable, helicon)
	log.Warn(
		"scheduled forced feeManager precompile disable at Helicon: pre-existing feeManager storage will be wiped at the Helicon activation block",
		"heliconTimestamp", helicon,
	)
	return out
}

func rejectUpgradesAtOrAfter(upgrades []extras.PrecompileUpgrade, helicon uint64) error {
	for i, up := range upgrades {
		if up.Key() != ConfigKey {
			continue
		}
		ts := up.Timestamp()
		if ts == nil || *ts < helicon {
			continue
		}
		kind := "Enable"
		if up.IsDisabled() {
			kind = "Disable"
		}
		return fmt.Errorf(
			"%w: %s upgrade at PrecompileUpgrades[%d] (timestamp=%d) (Helicon=%d); remove this entry from upgrades",
			ErrFeeManagerEnabledAfterHelicon, kind, i, *ts, helicon,
		)
	}
	return nil
}

func normalizeGenesisPrecompile(
	genesisPrecompiles map[string]precompileconfig.Config,
	genesisTimestamp uint64,
	helicon uint64,
) map[string]precompileconfig.Config {
	cfg, ok := genesisPrecompiles[ConfigKey]
	// IsDisabled is technically not possible here because the precompile cannot be disabled at genesis
	// but we check for it anyway to be safe; and defer to Config.Verify for the canonical error.
	if !ok || cfg.IsDisabled() {
		return genesisPrecompiles
	}
	ts := cfg.Timestamp()
	if ts == nil || *ts < helicon {
		return genesisPrecompiles
	}
	if genesisTimestamp >= helicon {
		// Fresh post-Helicon chain. Leave the entry so Config.Verify
		// rejects it with ErrFeeManagerEnabledAfterHelicon; the
		// operator can fix the genesis JSON before launch.
		return genesisPrecompiles
	}

	out := make(map[string]precompileconfig.Config, len(genesisPrecompiles)-1)
	for k, v := range genesisPrecompiles {
		if k == ConfigKey {
			continue
		}
		out[k] = v
	}
	log.Warn(
		"dropped feeManager genesis precompile scheduled at or after Helicon: precompile is retired in subnet-evm-sae",
		"feeManagerTimestamp", *ts,
		"heliconTimestamp", helicon,
		"genesisTimestamp", genesisTimestamp,
	)
	return out
}

// insertUpgradeBeforeFirstAfter inserts `upgrade` into the
// monotonic-non-decreasing `upgrades` slice immediately before the
// first entry with timestamp `> helicon`, preserving the global
// ordering enforced by [extras.ChainConfig.verifyPrecompileUpgrades].
//
// Caller must ensure no existing entry sharing `upgrade.Key()` sits at
// `>= helicon` to avoid tripping the same-key strict-monotonicity
// or alternation checks.
func insertUpgradeBeforeFirstAfter(
	upgrades []extras.PrecompileUpgrade,
	upgrade extras.PrecompileUpgrade,
	helicon uint64,
) []extras.PrecompileUpgrade {
	insertAt := len(upgrades)
	for i, existing := range upgrades {
		ts := existing.Timestamp()
		if ts != nil && *ts > helicon {
			insertAt = i
			break
		}
	}
	out := make([]extras.PrecompileUpgrade, 0, len(upgrades)+1)
	out = append(out, upgrades[:insertAt]...)
	out = append(out, upgrade)
	out = append(out, upgrades[insertAt:]...)
	return out
}
