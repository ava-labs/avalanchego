// (c) 2023 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/precompile/modules"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/utils"
	"github.com/ethereum/go-ethereum/common"
)

var errNoKey = errors.New("PrecompileUpgrade cannot be empty")

// PrecompileUpgrade is a helper struct embedded in UpgradeConfig.
// It is used to unmarshal the json into the correct precompile config type
// based on the key. Keys are defined in each precompile module, and registered in
// precompile/registry/registry.go.
type PrecompileUpgrade struct {
	precompileconfig.Config
}

// UnmarshalJSON unmarshals the json into the correct precompile config type
// based on the key. Keys are defined in each precompile module, and registered in
// precompile/registry/registry.go.
// Ex: {"feeManagerConfig": {...}} where "feeManagerConfig" is the key
func (u *PrecompileUpgrade) UnmarshalJSON(data []byte) error {
	raw := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		return errNoKey
	}
	if len(raw) > 1 {
		return fmt.Errorf("PrecompileUpgrade must have exactly one key, got %d", len(raw))
	}
	for key, value := range raw {
		module, ok := modules.GetPrecompileModule(key)
		if !ok {
			return fmt.Errorf("unknown precompile config: %s", key)
		}
		config := module.MakeConfig()
		if err := json.Unmarshal(value, config); err != nil {
			return err
		}
		u.Config = config
	}
	return nil
}

// MarshalJSON marshal the precompile config into json based on the precompile key.
// Ex: {"feeManagerConfig": {...}} where "feeManagerConfig" is the key
func (u *PrecompileUpgrade) MarshalJSON() ([]byte, error) {
	res := make(map[string]precompileconfig.Config)
	res[u.Key()] = u.Config
	return json.Marshal(res)
}

// verifyPrecompileUpgrades checks [c.PrecompileUpgrades] is well formed:
//   - [upgrades] must specify exactly one key per PrecompileUpgrade
//   - the specified blockTimestamps must monotonically increase
//   - the specified blockTimestamps must be compatible with those
//     specified in the chainConfig by genesis.
//   - check a precompile is disabled before it is re-enabled
func (c *ChainConfig) verifyPrecompileUpgrades() error {
	// Store this struct to keep track of the last upgrade for each precompile key.
	// Required for timestamp and disabled checks.
	type lastUpgradeData struct {
		blockTimestamp uint64
		disabled       bool
	}

	lastPrecompileUpgrades := make(map[string]lastUpgradeData)

	// next range over upgrades to verify correct use of disabled and blockTimestamps.
	// previousUpgradeTimestamp is used to verify monotonically increasing timestamps.
	var previousUpgradeTimestamp *uint64
	for i, upgrade := range c.PrecompileUpgrades {
		key := upgrade.Key()

		// lastUpgradeByKey is the previous processed upgrade for this precompile key.
		lastUpgradeByKey, ok := lastPrecompileUpgrades[key]
		var (
			disabled      bool
			lastTimestamp *uint64
		)
		if !ok {
			disabled = true
			lastTimestamp = nil
		} else {
			disabled = lastUpgradeByKey.disabled
			lastTimestamp = utils.NewUint64(lastUpgradeByKey.blockTimestamp)
		}
		upgradeTimestamp := upgrade.Timestamp()

		if upgradeTimestamp == nil {
			return fmt.Errorf("PrecompileUpgrade (%s) at [%d]: block timestamp cannot be nil ", key, i)
		}
		// Verify specified timestamps are monotonically increasing across all precompile keys.
		// Note: It is OK for multiple configs of DIFFERENT keys to specify the same timestamp.
		if previousUpgradeTimestamp != nil && *upgradeTimestamp < *previousUpgradeTimestamp {
			return fmt.Errorf("PrecompileUpgrade (%s) at [%d]: config block timestamp (%v) < previous timestamp (%v)", key, i, *upgradeTimestamp, *previousUpgradeTimestamp)
		}

		if disabled == upgrade.IsDisabled() {
			return fmt.Errorf("PrecompileUpgrade (%s) at [%d]: disable should be [%v]", key, i, !disabled)
		}
		// Verify specified timestamps are monotonically increasing across same precompile keys.
		// Note: It is NOT OK for multiple configs of the SAME key to specify the same timestamp.
		if lastTimestamp != nil && *upgradeTimestamp <= *lastTimestamp {
			return fmt.Errorf("PrecompileUpgrade (%s) at [%d]: config block timestamp (%v) <= previous timestamp (%v) of same key", key, i, *upgradeTimestamp, *lastTimestamp)
		}

		if err := upgrade.Verify(c); err != nil {
			return err
		}

		lastPrecompileUpgrades[key] = lastUpgradeData{
			disabled:       upgrade.IsDisabled(),
			blockTimestamp: *upgradeTimestamp,
		}

		previousUpgradeTimestamp = upgradeTimestamp
	}

	return nil
}

// getActivePrecompileConfig returns the most recent precompile config corresponding to [address].
// If none have occurred, returns nil.
func (c *ChainConfig) getActivePrecompileConfig(address common.Address, timestamp uint64) precompileconfig.Config {
	configs := c.GetActivatingPrecompileConfigs(address, nil, timestamp, c.PrecompileUpgrades)
	if len(configs) == 0 {
		return nil
	}
	return configs[len(configs)-1] // return the most recent config
}

// GetActivatingPrecompileConfigs returns all precompile upgrades configured to activate during the
// state transition from a block with timestamp [from] to a block with timestamp [to].
func (c *ChainConfig) GetActivatingPrecompileConfigs(address common.Address, from *uint64, to uint64, upgrades []PrecompileUpgrade) []precompileconfig.Config {
	// Get key from address.
	module, ok := modules.GetPrecompileModuleByAddress(address)
	if !ok {
		return nil
	}
	configs := make([]precompileconfig.Config, 0)
	key := module.ConfigKey
	// Loop over all upgrades checking for the requested precompile config.
	for _, upgrade := range upgrades {
		if upgrade.Key() == key {
			// Check if the precompile activates in the specified range.
			if IsForkTransition(upgrade.Timestamp(), from, to) {
				configs = append(configs, upgrade.Config)
			}
		}
	}
	return configs
}

// CheckPrecompilesCompatible checks if [precompileUpgrades] are compatible with [c] at [headTimestamp].
// Returns a ConfigCompatError if upgrades already activated at [headTimestamp] are missing from
// [precompileUpgrades]. Upgrades not already activated may be modified or absent from [precompileUpgrades].
// Returns nil if [precompileUpgrades] is compatible with [c].
// Assumes given timestamp is the last accepted block timestamp.
// This ensures that as long as the node has not accepted a block with a different rule set it will allow a
// new upgrade to be applied as long as it activates after the last accepted block.
func (c *ChainConfig) CheckPrecompilesCompatible(precompileUpgrades []PrecompileUpgrade, time uint64) *ConfigCompatError {
	for _, module := range modules.RegisteredModules() {
		if err := c.checkPrecompileCompatible(module.Address, precompileUpgrades, time); err != nil {
			return err
		}
	}

	return nil
}

// checkPrecompileCompatible verifies that the precompile specified by [address] is compatible between [c]
// and [precompileUpgrades] at [headTimestamp].
// Returns an error if upgrades already activated at [headTimestamp] are missing from [precompileUpgrades].
// Upgrades that have already gone into effect cannot be modified or absent from [precompileUpgrades].
func (c *ChainConfig) checkPrecompileCompatible(address common.Address, precompileUpgrades []PrecompileUpgrade, time uint64) *ConfigCompatError {
	// All active upgrades (from nil to [lastTimestamp]) must match.
	activeUpgrades := c.GetActivatingPrecompileConfigs(address, nil, time, c.PrecompileUpgrades)
	newUpgrades := c.GetActivatingPrecompileConfigs(address, nil, time, precompileUpgrades)

	// Check activated upgrades are still present.
	for i, upgrade := range activeUpgrades {
		if len(newUpgrades) <= i {
			// missing upgrade
			return newTimestampCompatError(
				fmt.Sprintf("missing PrecompileUpgrade[%d]", i),
				upgrade.Timestamp(),
				nil,
			)
		}
		// All upgrades that have activated must be identical.
		if !upgrade.Equal(newUpgrades[i]) {
			return newTimestampCompatError(
				fmt.Sprintf("PrecompileUpgrade[%d]", i),
				upgrade.Timestamp(),
				newUpgrades[i].Timestamp(),
			)
		}
	}
	// then, make sure newUpgrades does not have additional upgrades
	// that are already activated. (cannot perform retroactive upgrade)
	if len(newUpgrades) > len(activeUpgrades) {
		return newTimestampCompatError(
			fmt.Sprintf("cannot retroactively enable PrecompileUpgrade[%d]", len(activeUpgrades)),
			nil,
			newUpgrades[len(activeUpgrades)].Timestamp(), // this indexes to the first element in newUpgrades after the end of activeUpgrades
		)
	}

	return nil
}

// EnabledStatefulPrecompiles returns current stateful precompile configs that are enabled at [blockTimestamp].
func (c *ChainConfig) EnabledStatefulPrecompiles(blockTimestamp uint64) Precompiles {
	statefulPrecompileConfigs := make(Precompiles)
	for _, module := range modules.RegisteredModules() {
		if config := c.getActivePrecompileConfig(module.Address, blockTimestamp); config != nil && !config.IsDisabled() {
			statefulPrecompileConfigs[module.ConfigKey] = config
		}
	}

	return statefulPrecompileConfigs
}
