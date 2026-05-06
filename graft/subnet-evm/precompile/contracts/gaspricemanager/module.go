// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var _ contract.Configurator = (*configurator)(nil)

// ConfigKey must be unique across all precompiles.
const ConfigKey = "gasPriceManagerConfig"

var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000006")

var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     gasPriceManagerPrecompile,
	Configurator: &configurator{},
}

type configurator struct{}

func init() {
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

// MakeConfig returns a zero-valued config for JSON unmarshaling when
// loading this precompile's config from chain config or upgrades.
func (*configurator) MakeConfig() precompileconfig.Config {
	return new(Config)
}

// Configure performs one-time initialization when the precompile activates.
// It seeds contract storage with the initial gas price config and allowlist state.
// This function MUST only be called once as re-running it would reset that
// initialized state.
func (*configurator) Configure(chainConfig precompileconfig.ChainConfig, cfg precompileconfig.Config, state contract.StateDB, blockContext contract.ConfigurationBlockContext) error {
	config, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	initialGasPriceConfig := commontype.DefaultGasPriceConfig()
	if config.InitialGasPriceConfig != nil {
		initialGasPriceConfig = *config.InitialGasPriceConfig
	}
	if err := StoreGasPriceConfig(state, ContractAddress, initialGasPriceConfig, blockContext.Number()); err != nil {
		return fmt.Errorf("cannot configure initial gas price config: %w", err)
	}
	return config.AllowListConfig.Configure(chainConfig, ContractAddress, state, blockContext)
}
