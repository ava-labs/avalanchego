// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package retirement

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/utils"
)

// ErrFeeManagerEnabledAfterHelicon is returned when a feeManager configuration
// is scheduled at or after Helicon.
var ErrFeeManagerEnabledAfterHelicon = errors.New("feeManager precompile cannot be enabled at or after Helicon")

// ReconcileForHelicon removes feeManager configurations that cannot survive
// Helicon and schedules a disable when the precompile was active before it.
func ReconcileForHelicon(cfg *extras.ChainConfig, genesisTimestamp, helicon uint64) error {
	if err := rejectUpgradesAtOrAfter(cfg.PrecompileUpgrades, helicon); err != nil {
		return err
	}

	if genesisConfig, ok := cfg.GenesisPrecompiles[feemanager.ConfigKey]; ok && !genesisConfig.IsDisabled() {
		if genesisTimestamp >= helicon {
			return fmt.Errorf(
				"%w: feeManager configured in post-Helicon genesis",
				ErrFeeManagerEnabledAfterHelicon,
			)
		}
		if ts := genesisConfig.Timestamp(); ts != nil && *ts >= helicon {
			cfg.GenesisPrecompiles = maps.Clone(cfg.GenesisPrecompiles)
			delete(cfg.GenesisPrecompiles, feemanager.ConfigKey)
			log.Warn(
				"dropped feeManager genesis precompile scheduled at or after Helicon",
				"feeManagerTimestamp", *ts,
				"heliconTimestamp", helicon,
				"genesisTimestamp", genesisTimestamp,
			)
		}
	}

	if !cfg.IsPrecompileEnabled(feemanager.ContractAddress, helicon) {
		return nil
	}
	disable := extras.PrecompileUpgrade{
		Config: feemanager.NewDisableConfig(utils.PointerTo(helicon)),
	}
	insertAt := len(cfg.PrecompileUpgrades)
	for i, upgrade := range cfg.PrecompileUpgrades {
		if ts := upgrade.Timestamp(); ts != nil && *ts > helicon {
			insertAt = i
			break
		}
	}
	cfg.PrecompileUpgrades = slices.Insert(cfg.PrecompileUpgrades, insertAt, disable)
	log.Warn(
		"scheduled feeManager disable at Helicon",
		"heliconTimestamp", helicon,
	)
	return nil
}

func rejectUpgradesAtOrAfter(upgrades []extras.PrecompileUpgrade, helicon uint64) error {
	for i, upgrade := range upgrades {
		if upgrade.Key() != feemanager.ConfigKey {
			continue
		}
		ts := upgrade.Timestamp()
		if ts == nil || *ts < helicon {
			continue
		}
		return fmt.Errorf(
			"%w: upgrade at precompileUpgrades[%d] has timestamp %d (Helicon=%d)",
			ErrFeeManagerEnabledAfterHelicon,
			i,
			*ts,
			helicon,
		)
	}
	return nil
}
