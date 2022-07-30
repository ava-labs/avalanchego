// (c) 2022 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ava-labs/subnet-evm/utils"
)

// precompileKey is a helper type used to reference each of the
// possible stateful precompile types that can be activated
// as a network upgrade.
type precompileKey int

const (
	contractDeployerAllowListKey precompileKey = iota + 1
	contractNativeMinterKey
	txAllowListKey
	feeManagerKey
)

var (
	precompileKeys = []precompileKey{contractDeployerAllowListKey, contractNativeMinterKey, txAllowListKey, feeManagerKey}
)

// PrecompileUpgrade is a helper struct embedded in UpgradeConfig, representing
// each of the possible stateful precompile types that can be activated
// as a network upgrade.
type PrecompileUpgrade struct {
	ContractDeployerAllowListConfig *precompile.ContractDeployerAllowListConfig `json:"contractDeployerAllowListConfig,omitempty"` // Config for the contract deployer allow list precompile
	ContractNativeMinterConfig      *precompile.ContractNativeMinterConfig      `json:"contractNativeMinterConfig,omitempty"`      // Config for the native minter precompile
	TxAllowListConfig               *precompile.TxAllowListConfig               `json:"txAllowListConfig,omitempty"`               // Config for the tx allow list precompile
	FeeManagerConfig                *precompile.FeeConfigManagerConfig          `json:"feeManagerConfig,omitempty"`                // Config for the fee manager precompile
}

func (p *PrecompileUpgrade) getByKey(key precompileKey) (precompile.StatefulPrecompileConfig, bool) {
	switch key {
	case contractDeployerAllowListKey:
		return p.ContractDeployerAllowListConfig, p.ContractDeployerAllowListConfig != nil
	case contractNativeMinterKey:
		return p.ContractNativeMinterConfig, p.ContractNativeMinterConfig != nil
	case txAllowListKey:
		return p.TxAllowListConfig, p.TxAllowListConfig != nil
	case feeManagerKey:
		return p.FeeManagerConfig, p.FeeManagerConfig != nil
	default:
		panic(fmt.Sprintf("unknown upgrade key: %v", key))
	}
}

// VerifyPrecompileUpgrades checks [c.PrecompileUpgrades] is well formed:
// - [upgrades] must specify exactly one key per PrecompileUpgrade
// - the specified blockTimestamps must monotonically increase
// - the specified blockTimestamps must be compatible with those
//   specified in the chainConfig by genesis.
// - check a precompile is disabled before it is re-enabled
func (c *ChainConfig) VerifyPrecompileUpgrades() error {
	var lastBlockTimestamp *big.Int
	for i, upgrade := range c.PrecompileUpgrades {
		hasKey := false // used to verify if there is only one key per Upgrade

		for _, key := range precompileKeys {
			config, ok := upgrade.getByKey(key)
			if !ok {
				continue
			}
			if hasKey {
				return fmt.Errorf("PrecompileUpgrades[%d] has more than one key set", i)
			}
			configTimestamp := config.Timestamp()
			if configTimestamp == nil {
				return fmt.Errorf("PrecompileUpgrades[%d] cannot have a nil timestamp", i)
			}
			// Verify specified timestamps are monotonically increasing across all precompile keys.
			// Note: It is OK for multiple configs of different keys to specify the same timestamp.
			if lastBlockTimestamp != nil && configTimestamp.Cmp(lastBlockTimestamp) < 0 {
				return fmt.Errorf("PrecompileUpgrades[%d] config timestamp (%v) < previous timestamp (%v)", i, configTimestamp, lastBlockTimestamp)
			}
			lastBlockTimestamp = configTimestamp
			hasKey = true
		}
		if !hasKey {
			return fmt.Errorf("empty precompile upgrade at index %d", i)
		}
	}

	for _, key := range precompileKeys {
		var (
			lastUpgraded *big.Int
			disabled     bool
		)
		// check the genesis chain config for any enabled upgrade
		if config, ok := c.PrecompileUpgrade.getByKey(key); ok {
			disabled = false
			lastUpgraded = config.Timestamp()
		} else {
			disabled = true
		}
		// next range over upgrades to verify correct use of disabled and blockTimestamps.
		for i, upgrade := range c.PrecompileUpgrades {
			config, ok := upgrade.getByKey(key)
			// Skip the upgrade if it's not relevant to [key].
			if !ok {
				continue
			}

			if disabled == config.IsDisabled() {
				return fmt.Errorf("PrecompileUpgrades[%d] disable should be [%v]", i, !disabled)
			}
			if lastUpgraded != nil && (config.Timestamp().Cmp(lastUpgraded) <= 0) {
				return fmt.Errorf("PrecompileUpgrades[%d] config timestamp (%v) <= previous timestamp (%v)", i, config.Timestamp(), lastUpgraded)
			}

			disabled = config.IsDisabled()
			lastUpgraded = config.Timestamp()
		}
	}

	return nil
}

// getActivePrecompileConfig returns the most recent precompile config corresponding to [key].
// If none have occurred, returns nil.
func (c *ChainConfig) getActivePrecompileConfig(blockTimestamp *big.Int, key precompileKey, upgrades []PrecompileUpgrade) precompile.StatefulPrecompileConfig {
	configs := c.getActivatingPrecompileConfigs(nil, blockTimestamp, key, upgrades)
	if len(configs) == 0 {
		return nil
	}
	return configs[len(configs)-1] // return the most recent config
}

// getActivatingPrecompileConfigs returns all forks configured to activate during the state transition from a block with timestamp [from]
// to a block with timestamp [to].
func (c *ChainConfig) getActivatingPrecompileConfigs(from *big.Int, to *big.Int, key precompileKey, upgrades []PrecompileUpgrade) []precompile.StatefulPrecompileConfig {
	configs := make([]precompile.StatefulPrecompileConfig, 0)
	// First check the embedded [upgrade] for precompiles configured
	// in the genesis chain config.
	if config, ok := c.PrecompileUpgrade.getByKey(key); ok {
		if utils.IsForkTransition(config.Timestamp(), from, to) {
			configs = append(configs, config)
		}
	}
	// Loop over all upgrades checking for the requested precompile config.
	for _, upgrade := range upgrades {
		if config, ok := upgrade.getByKey(key); ok {
			// Check if the precompile activates in the specified range.
			if utils.IsForkTransition(config.Timestamp(), from, to) {
				configs = append(configs, config)
			}
		}
	}
	return configs
}

// GetContractDeployerAllowListConfig returns the latest forked ContractDeployerAllowListConfig
// specified by [c] or nil if it was never enabled.
func (c *ChainConfig) GetContractDeployerAllowListConfig(blockTimestamp *big.Int) *precompile.ContractDeployerAllowListConfig {
	if val := c.getActivePrecompileConfig(blockTimestamp, contractDeployerAllowListKey, c.PrecompileUpgrades); val != nil {
		return val.(*precompile.ContractDeployerAllowListConfig)
	}
	return nil
}

// GetContractNativeMinterConfig returns the latest forked ContractNativeMinterConfig
// specified by [c] or nil if it was never enabled.
func (c *ChainConfig) GetContractNativeMinterConfig(blockTimestamp *big.Int) *precompile.ContractNativeMinterConfig {
	if val := c.getActivePrecompileConfig(blockTimestamp, contractNativeMinterKey, c.PrecompileUpgrades); val != nil {
		return val.(*precompile.ContractNativeMinterConfig)
	}
	return nil
}

// GetTxAllowListConfig returns the latest forked TxAllowListConfig
// specified by [c] or nil if it was never enabled.
func (c *ChainConfig) GetTxAllowListConfig(blockTimestamp *big.Int) *precompile.TxAllowListConfig {
	if val := c.getActivePrecompileConfig(blockTimestamp, txAllowListKey, c.PrecompileUpgrades); val != nil {
		return val.(*precompile.TxAllowListConfig)
	}
	return nil
}

// GetFeeConfigManagerConfig returns the latest forked FeeManagerConfig
// specified by [c] or nil if it was never enabled.
func (c *ChainConfig) GetFeeConfigManagerConfig(blockTimestamp *big.Int) *precompile.FeeConfigManagerConfig {
	if val := c.getActivePrecompileConfig(blockTimestamp, feeManagerKey, c.PrecompileUpgrades); val != nil {
		return val.(*precompile.FeeConfigManagerConfig)
	}
	return nil
}

// CheckPrecompilesCompatible checks if [precompileUpgrades] are compatible with [c] at [headTimestamp].
// Returns a ConfigCompatError if upgrades already forked at [headTimestamp] are missing from
// [precompileUpgrades]. Upgrades not already forked may be modified or absent from [precompileUpgrades].
// Returns nil if [precompileUpgrades] is compatible with [c].
func (c *ChainConfig) CheckPrecompilesCompatible(precompileUpgrades []PrecompileUpgrade, headTimestamp *big.Int) *ConfigCompatError {
	for _, key := range precompileKeys {
		if err := c.checkPrecompileCompatible(key, precompileUpgrades, headTimestamp); err != nil {
			return err
		}
	}

	return nil
}

// checkPrecompileCompatible verifies that the precompile specified by [key] is compatible between [c] and [precompileUpgrades] at [headTimestamp].
// Returns an error if upgrades already forked at [headTimestamp] are missing from [precompileUpgrades].
// Upgrades that have already gone into effect cannot be modified or absent from [precompileUpgrades].
func (c *ChainConfig) checkPrecompileCompatible(key precompileKey, precompileUpgrades []PrecompileUpgrade, headTimestamp *big.Int) *ConfigCompatError {
	// all active upgrades must match
	activeUpgrades := c.getActivatingPrecompileConfigs(nil, headTimestamp, key, c.PrecompileUpgrades)
	newUpgrades := c.getActivatingPrecompileConfigs(nil, headTimestamp, key, precompileUpgrades)

	// first, check existing upgrades are there
	for i, upgrade := range activeUpgrades {
		if len(newUpgrades) <= i {
			// missing upgrade
			return newCompatError(
				fmt.Sprintf("missing PrecompileUpgrade[%d]", i),
				upgrade.Timestamp(),
				nil,
			)
		}
		// All upgrades that have forked must be identical.
		if !upgrade.Equal(newUpgrades[i]) {
			return newCompatError(
				fmt.Sprintf("PrecompileUpgrade[%d]", i),
				upgrade.Timestamp(),
				newUpgrades[i].Timestamp(),
			)
		}
	}
	// then, make sure newUpgrades does not have additional upgrades
	// that are already activated. (cannot perform retroactive upgrade)
	if len(newUpgrades) > len(activeUpgrades) {
		return newCompatError(
			fmt.Sprintf("cannot retroactively enable PrecompileUpgrade[%d]", len(activeUpgrades)),
			nil,
			newUpgrades[len(activeUpgrades)].Timestamp(), // this indexes to the first element in newUpgrades after the end of activeUpgrades
		)
	}

	return nil
}

// EnabledStatefulPrecompiles returns a slice of stateful precompile configs that
// have been activated through an upgrade.
func (c *ChainConfig) EnabledStatefulPrecompiles(blockTimestamp *big.Int) []precompile.StatefulPrecompileConfig {
	statefulPrecompileConfigs := make([]precompile.StatefulPrecompileConfig, 0)
	for _, key := range precompileKeys {
		if config := c.getActivePrecompileConfig(blockTimestamp, key, c.PrecompileUpgrades); config != nil {
			statefulPrecompileConfigs = append(statefulPrecompileConfigs, config)
		}
	}

	return statefulPrecompileConfigs
}

// CheckConfigurePrecompiles checks if any of the precompiles specified by the chain config are enabled or disabled by the block
// transition from [parentTimestamp] to the timestamp set in [blockContext]. If this is the case, it calls [Configure]
// or [Deconfigure] to apply the necessary state transitions for the upgrade.
// This function is called:
// - within genesis setup to configure the starting state for precompiles enabled at genesis,
// - during block processing to update the state before processing the given block.
func (c *ChainConfig) CheckConfigurePrecompiles(parentTimestamp *big.Int, blockContext precompile.BlockContext, statedb precompile.StateDB) {
	blockTimestamp := blockContext.Timestamp()
	for _, key := range precompileKeys { // Note: configure precompiles in a deterministic order.
		for _, config := range c.getActivatingPrecompileConfigs(parentTimestamp, blockTimestamp, key, c.PrecompileUpgrades) {
			// If this transition activates the upgrade, configure the stateful precompile.
			// (or deconfigure it if it is being disabled.)
			if config.IsDisabled() {
				statedb.Suicide(config.Address())
			} else {
				precompile.Configure(c, blockContext, config, statedb)
			}
		}
	}
}
