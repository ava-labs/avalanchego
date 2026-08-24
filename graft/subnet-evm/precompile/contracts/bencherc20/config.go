// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bencherc20

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var _ precompileconfig.Config = (*Config)(nil)

// Config activates the bench ERC-20 precompile and names the token owner, the
// only address allowed to mint, pause, and blocklist.
type Config struct {
	precompileconfig.Upgrade
	Owner common.Address `json:"owner"`
}

func (*Config) Key() string { return ConfigKey }

func (c *Config) Equal(cfg precompileconfig.Config) bool {
	other, ok := (cfg).(*Config)
	if !ok {
		return false
	}
	return c.Upgrade.Equal(&other.Upgrade) && c.Owner == other.Owner
}

func (*Config) Verify(precompileconfig.ChainConfig) error { return nil }
