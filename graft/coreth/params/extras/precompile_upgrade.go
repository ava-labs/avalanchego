// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"encoding/json"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

// PrecompileUpgrade is a helper struct embedded in UpgradeConfig.
// It is used to unmarshal the json into the correct precompile config type
// based on the key. Keys are defined in each precompile module, and registered in
// precompile/registry/registry.go.
type PrecompileUpgrade struct {
	precompileconfig.Config
}

// MarshalJSON marshal the precompile config into json based on the precompile key.
// Ex: {"feeManagerConfig": {...}} where "feeManagerConfig" is the key
func (u *PrecompileUpgrade) MarshalJSON() ([]byte, error) {
	res := make(map[string]precompileconfig.Config)
	res[u.Key()] = u.Config
	return json.Marshal(res)
}

// GetActivePrecompileConfig returns the most recent precompile config corresponding to [address].
// If none have occurred, returns nil.
func (c *ChainConfig) GetActivePrecompileConfig(address common.Address, timestamp uint64) precompileconfig.Config {
	configs := c.GetActivatingPrecompileConfigs(address, nil, timestamp, c.PrecompileUpgrades)
	if len(configs) == 0 {
		return nil
	}
	return configs[len(configs)-1] // return the most recent config
}

// GetActivatingPrecompileConfigs returns all precompile upgrades configured to activate during the
// state transition from a block with timestamp [from] to a block with timestamp [to].
func (*ChainConfig) GetActivatingPrecompileConfigs(address common.Address, from *uint64, to uint64, upgrades []PrecompileUpgrade) []precompileconfig.Config {
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
