// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package crosschain implements the C-Chain cross-chain transfer precompile.
// An EVM call is the import or the export; see vms/saevm/cchain/hooks.go for
// the block verifier and shared-memory side.
package crosschain

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

const ConfigKey = "crossChainTransferConfig"

var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000007")

var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     Precompile,
	Configurator: &configurator{},
}

func init() {
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

var _ precompileconfig.Config = (*Config)(nil)

type Config struct {
	precompileconfig.Upgrade
}

func NewConfig(blockTimestamp *uint64) *Config {
	return &Config{Upgrade: precompileconfig.Upgrade{BlockTimestamp: blockTimestamp}}
}

func (*Config) Key() string                               { return ConfigKey }
func (*Config) Verify(precompileconfig.ChainConfig) error { return nil }

func (c *Config) Equal(s precompileconfig.Config) bool {
	other, ok := s.(*Config)
	return ok && c.Upgrade.Equal(&other.Upgrade)
}

type configurator struct{}

func (*configurator) MakeConfig() precompileconfig.Config { return new(Config) }

func (*configurator) Configure(_ precompileconfig.ChainConfig, cfg precompileconfig.Config, _ contract.StateDB, _ contract.ConfigurationBlockContext) error {
	if _, ok := cfg.(*Config); !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	return nil
}
