// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/stateupgrade"

	ethparams "github.com/ava-labs/libevm/params"
)

var (
	errStateUpgradeNilTimestamp          = errors.New("state upgrade config block timestamp cannot be nil")
	errStateUpgradeTimestampZero         = errors.New("state upgrade config block timestamp must be greater than 0")
	errStateUpgradeTimestampNotMonotonic = errors.New("state upgrade config block timestamp must be greater than previous timestamp")
)

// verifyStateUpgrades checks [c.StateUpgrades] is well formed:
// - the specified blockTimestamps must monotonically increase
func (c *ChainConfig) verifyStateUpgrades() error {
	var previousUpgradeTimestamp *uint64
	for i, upgrade := range c.StateUpgrades {
		upgradeTimestamp := upgrade.Timestamp
		if upgradeTimestamp == nil {
			return fmt.Errorf("%w: StateUpgrade[%d]", errStateUpgradeNilTimestamp, i)
		}
		// Verify the upgrade's timestamp is equal 0 (to avoid confusion with genesis).
		if *upgradeTimestamp == 0 {
			return fmt.Errorf("%w: StateUpgrade[%d] has timestamp %v", errStateUpgradeTimestampZero, i, *upgradeTimestamp)
		}

		// Verify specified timestamps are strictly monotonically increasing.
		if previousUpgradeTimestamp != nil && *upgradeTimestamp <= *previousUpgradeTimestamp {
			return fmt.Errorf("%w: StateUpgrade[%d] has timestamp %v, previous timestamp %v", errStateUpgradeTimestampNotMonotonic, i, *upgradeTimestamp, *previousUpgradeTimestamp)
		}
		previousUpgradeTimestamp = upgradeTimestamp
	}
	return nil
}

// GetActivatingStateUpgrades returns all state upgrades configured to activate during the
// state transition from a block with timestamp [from] to a block with timestamp [to].
func (c *ChainConfig) GetActivatingStateUpgrades(from *uint64, to uint64) []stateupgrade.Block {
	return getActivatingStateUpgrades(from, to, c.StateUpgrades)
}

func getActivatingStateUpgrades(from *uint64, to uint64, upgrades []stateupgrade.Block) []stateupgrade.Block {
	var activating []stateupgrade.Block
	for _, upgrade := range upgrades {
		if IsForkTransition(upgrade.Timestamp, from, to) {
			activating = append(activating, upgrade)
		}
	}
	return activating
}

// checkStateUpgradesCompatible checks if [stateUpgrades] are compatible with [c] at [headTimestamp].
func (c *ChainConfig) checkStateUpgradesCompatible(stateUpgrades []stateupgrade.Block, lastTimestamp uint64) *ethparams.ConfigCompatError {
	// All active upgrades (from nil to [lastTimestamp]) must match.
	activeUpgrades := getActivatingStateUpgrades(nil, lastTimestamp, c.StateUpgrades)
	newUpgrades := getActivatingStateUpgrades(nil, lastTimestamp, stateUpgrades)

	// Check activated upgrades are still present.
	for i, upgrade := range activeUpgrades {
		if len(newUpgrades) <= i {
			// missing upgrade
			return ethparams.NewTimestampCompatError(
				fmt.Sprintf("missing StateUpgrade[%d]", i),
				upgrade.Timestamp,
				nil,
			)
		}
		// All upgrades that have activated must be identical.
		if !upgrade.Equal(&newUpgrades[i]) {
			return ethparams.NewTimestampCompatError(
				fmt.Sprintf("StateUpgrade[%d]", i),
				upgrade.Timestamp,
				newUpgrades[i].Timestamp,
			)
		}
	}
	// then, make sure newUpgrades does not have additional upgrades
	// that are already activated. (cannot perform retroactive upgrade)
	if len(newUpgrades) > len(activeUpgrades) {
		return ethparams.NewTimestampCompatError(
			fmt.Sprintf("cannot retroactively enable StateUpgrade[%d]", len(activeUpgrades)),
			nil,
			newUpgrades[len(activeUpgrades)].Timestamp, // this indexes to the first element in newUpgrades after the end of activeUpgrades
		)
	}

	return nil
}
