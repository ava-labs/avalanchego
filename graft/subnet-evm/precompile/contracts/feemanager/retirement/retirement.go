// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package retirement

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
)

// ErrFeeManagerEnabledAfterIgloo is returned when a feeManager configuration
// is scheduled at or after Igloo.
var ErrFeeManagerEnabledAfterIgloo = errors.New("feeManager precompile cannot be enabled at or after Igloo")

// ReconcileForIgloo removes feeManager configurations that cannot survive
// Igloo and schedules a disable when the precompile was active before it.
func ReconcileForIgloo(cfg *extras.ChainConfig, genesisTimestamp, igloo uint64) error {
	if err := rejectUpgradesAtOrAfter(cfg.PrecompileUpgrades, igloo); err != nil {
		return err
	}

	if genesisConfig, ok := cfg.GenesisPrecompiles[feemanager.ConfigKey]; ok && !genesisConfig.IsDisabled() {
		if genesisTimestamp >= igloo {
			return fmt.Errorf(
				"%w: feeManager configured in post-Igloo genesis",
				ErrFeeManagerEnabledAfterIgloo,
			)
		}
		if ts := genesisConfig.Timestamp(); ts != nil && *ts >= igloo {
			cfg.GenesisPrecompiles = maps.Clone(cfg.GenesisPrecompiles)
			delete(cfg.GenesisPrecompiles, feemanager.ConfigKey)
			log.Warn(
				"dropped feeManager genesis precompile scheduled at or after Igloo",
				"feeManagerTimestamp", *ts,
				"iglooTimestamp", igloo,
				"genesisTimestamp", genesisTimestamp,
			)
		}
	}

	if !cfg.IsPrecompileEnabled(feemanager.ContractAddress, igloo) {
		return nil
	}
	disable := extras.PrecompileUpgrade{
		Config: feemanager.NewDisableConfig(utils.PointerTo(igloo)),
	}
	insertAt := len(cfg.PrecompileUpgrades)
	for i, upgrade := range cfg.PrecompileUpgrades {
		if ts := upgrade.Timestamp(); ts != nil && *ts > igloo {
			insertAt = i
			break
		}
	}
	cfg.PrecompileUpgrades = slices.Insert(cfg.PrecompileUpgrades, insertAt, disable)
	log.Warn(
		"scheduled feeManager disable at Igloo",
		"iglooTimestamp", igloo,
	)
	return nil
}

func rejectUpgradesAtOrAfter(upgrades []extras.PrecompileUpgrade, igloo uint64) error {
	for i, upgrade := range upgrades {
		if upgrade.Key() != feemanager.ConfigKey {
			continue
		}
		ts := upgrade.Timestamp()
		if ts == nil || *ts < igloo {
			continue
		}
		return fmt.Errorf(
			"%w: upgrade at precompileUpgrades[%d] has timestamp %d (Igloo=%d)",
			ErrFeeManagerEnabledAfterIgloo,
			i,
			*ts,
			igloo,
		)
	}
	return nil
}
