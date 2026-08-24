// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bencherc20

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
)

var _ contract.Configurator = (*configurator)(nil)

// ConfigKey is the key used in json config files to specify this precompile config.
const ConfigKey = "benchErc20Config"

// ContractAddress is in the 0x03 range reserved for subnet-evm forks.
var ContractAddress = common.HexToAddress("0x0300000000000000000000000000000000000001")

var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     BenchERC20Precompile,
	Configurator: &configurator{},
}

type configurator struct{}

func init() {
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

func (*configurator) MakeConfig() precompileconfig.Config {
	return new(Config)
}

// Configure writes the token owner into the precompile's storage at activation.
func (*configurator) Configure(_ precompileconfig.ChainConfig, cfg precompileconfig.Config, state contract.StateDB, _ contract.ConfigurationBlockContext) error {
	config, ok := cfg.(*Config)
	if !ok {
		return fmt.Errorf("expected config type %T, got %T: %v", &Config{}, cfg, cfg)
	}
	state.SetState(ContractAddress, ownerSlot, common.BytesToHash(config.Owner.Bytes()))
	return nil
}
