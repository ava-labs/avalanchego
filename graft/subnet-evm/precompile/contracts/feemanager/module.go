// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package feemanager

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
)

var _ contract.Configurator = (*configurator)(nil)

// ConfigKey is the key used in json config files to specify this precompile config.
// must be unique across all precompiles.
const ConfigKey = "feeManagerConfig"

var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000003")

// Module is the precompile module. It is used to register the precompile contract.
var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     FeeManagerPrecompile,
	Configurator: &configurator{},
}

type configurator struct{}

func init() {
	// Register the precompile module.
	// Each precompile contract registers itself through [RegisterModule] function.
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

// MakeConfig returns a new precompile config instance.
// This is required to Marshal/Unmarshal the precompile config.
func (*configurator) MakeConfig() precompileconfig.Config {
	return new(Config)
}

var errNoFeeConfig = errors.New("chain config does not have a fee config")

// Configure configures [state] with the given [cfg] precompileconfig.
// This function is called by the EVM once per precompile contract activation.
func (*configurator) Configure(chainConfig precompileconfig.ChainConfig, cfg precompileconfig.Config, state contract.StateDB, blockContext contract.ConfigurationBlockContext) error {
	config, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	// Store the initial fee config into the state when the fee manager activates.
	if config.InitialFeeConfig != nil {
		if err := StoreFeeConfig(state, *config.InitialFeeConfig, blockContext); err != nil {
			// This should not happen since we already checked this config with Verify()
			return fmt.Errorf("cannot configure given initial fee config: %w", err)
		}
	} else {
		feeCfg := chainConfig.GetFeeConfig()
		if feeCfg == nil {
			return errNoFeeConfig
		}
		if err := StoreFeeConfig(state, *feeCfg, blockContext); err != nil {
			// This should not happen since we already checked the chain config in the genesis creation.
			return fmt.Errorf("cannot configure fee config in chain config: %w", err)
		}
	}
	return config.AllowListConfig.Configure(chainConfig, ContractAddress, state, blockContext)
}
