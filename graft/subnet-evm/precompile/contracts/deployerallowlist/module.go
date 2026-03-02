// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package deployerallowlist

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var _ contract.Configurator = (*configurator)(nil)

// ConfigKey is the key used in json config files to specify this precompile config.
// must be unique across all precompiles.
const ConfigKey = "contractDeployerAllowListConfig"

var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000000")

var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     ContractDeployerAllowListPrecompile,
	Configurator: &configurator{},
}

type configurator struct{}

func init() {
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

// MakeConfig returns a new precompile config instance.
// This is required to Marshal/Unmarshal the precompile config.
func (*configurator) MakeConfig() precompileconfig.Config {
	return new(Config)
}

// Configure configures [state] with the given [cfg] precompileconfig.
// This function is called by the EVM once per precompile contract activation.
func (*configurator) Configure(chainConfig precompileconfig.ChainConfig, cfg precompileconfig.Config, state contract.StateDB, blockContext contract.ConfigurationBlockContext) error {
	config, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	return config.AllowListConfig.Configure(chainConfig, ContractAddress, state, blockContext)
}
