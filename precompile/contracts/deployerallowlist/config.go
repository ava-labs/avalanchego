// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
)

var _ precompileconfig.Config = (*Config)(nil)

// Config contains the configuration for the ContractDeployerAllowList precompile,
// consisting of the initial allowlist and the timestamp for the network upgrade.
type Config struct {
	allowlist.AllowListConfig
	precompileconfig.Upgrade
}

// NewConfig returns a config for a network upgrade at [blockTimestamp] that enables
// ContractDeployerAllowList with [admins], [enableds] and [managers] as members of the allowlist.
func NewConfig(blockTimestamp *uint64, admins []common.Address, enableds []common.Address, managers []common.Address) *Config {
	return &Config{
		AllowListConfig: allowlist.AllowListConfig{
			AdminAddresses:   admins,
			EnabledAddresses: enableds,
			ManagerAddresses: managers,
		},
		Upgrade: precompileconfig.Upgrade{BlockTimestamp: blockTimestamp},
	}
}

// NewDisableConfig returns config for a network upgrade at [blockTimestamp]
// that disables ContractDeployerAllowList.
func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

func (*Config) Key() string { return ConfigKey }

// Equal returns true if [cfg] is a [*ContractDeployerAllowListConfig] and it has been configured identical to [c].
func (c *Config) Equal(cfg precompileconfig.Config) bool {
	// typecast before comparison
	other, ok := (cfg).(*Config)
	if !ok {
		return false
	}
	return c.Upgrade.Equal(&other.Upgrade) && c.AllowListConfig.Equal(&other.AllowListConfig)
}

func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	return c.AllowListConfig.Verify(chainConfig, c.Upgrade)
}
