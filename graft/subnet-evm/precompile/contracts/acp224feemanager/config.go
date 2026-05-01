// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/allowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var _ precompileconfig.Config = (*Config)(nil)

// Config is the configuration for the ACP-224 fee manager precompile.
// It specifies:
//   - when the precompile is activated ([precompileconfig.Upgrade])
//   - who may call it ([allowlist.AllowListConfig])
//   - an optional initial fee config ([commontype.ACP224FeeConfig]) to write to contract storage on activation.
type Config struct {
	allowlist.AllowListConfig
	precompileconfig.Upgrade
	InitialFeeConfig *commontype.ACP224FeeConfig `json:"initialFeeConfig,omitempty"` // activated immediately on precompile enable if provided
}

// NewConfig returns a config that enables ACP224FeeManager at `blockTimestamp`.
func NewConfig(blockTimestamp *uint64, admins []common.Address, enabled []common.Address, managers []common.Address, initialConfig *commontype.ACP224FeeConfig) *Config {
	return &Config{
		AllowListConfig: allowlist.AllowListConfig{
			AdminAddresses:   admins,
			EnabledAddresses: enableds,
			ManagerAddresses: managers,
		},
		Upgrade:          precompileconfig.Upgrade{BlockTimestamp: blockTimestamp},
		InitialFeeConfig: initialConfig,
	}
}

// NewDisableConfig returns a config that disables ACP224FeeManager at `blockTimestamp`.
func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Key must match ConfigKey used in the precompile module.
func (*Config) Key() string { return ConfigKey }

// Equal returns true if [cfg] is a *Config identical to [c].
func (c *Config) Equal(cfg precompileconfig.Config) bool {
	if c == nil {
		return cfg == nil
	}
	other, ok := (cfg).(*Config)
	if !ok || other == nil {
		return false
	}
	eq := c.Upgrade.Equal(&other.Upgrade) && c.AllowListConfig.Equal(&other.AllowListConfig)
	if !eq {
		return false
	}

	return c.InitialFeeConfig.Equal(other.InitialFeeConfig)
}

// Verify validates the allow list config and, if set, the initial fee config.
func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	if err := c.AllowListConfig.Verify(chainConfig, c.Upgrade); err != nil {
		return err
	}
	if c.InitialFeeConfig == nil {
		return nil
	}

	return c.InitialFeeConfig.Verify()
}
