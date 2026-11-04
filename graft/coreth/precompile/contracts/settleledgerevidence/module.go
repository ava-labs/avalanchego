// Package settleledger measures what moving settlement work out of the EVM buys.
//
// The settlement row is dominated by storage writes, logs and cross-contract calls
// rather than by computation, so it is not obvious that a Go implementation is
// faster: a precompile still writes through the same StateDB, and gas prices state
// growth rather than CPU. This package isolates the three cost classes so each can
// be measured against its Solidity equivalent instead of argued about.
//
// It also makes the gas question explicit. A precompile charges whatever its author
// charges, so measuring only gas would confirm any hypothesis. Each method here is
// offered at both the EVM's own price and at a nominal price, so a change in
// throughput can be attributed to execution or to pricing rather than conflated.
package settleledger

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
)

const ConfigKey = "settleLedgerConfig"

// Forks of subnet-evm are asked to start at 0x0300... so they cannot collide with
// upstream precompiles or with anything in core/vm.
var ContractAddress = common.HexToAddress("0x0300000000000000000000000000000000000002")

var Module = modules.Module{
	ConfigKey:    ConfigKey,
	Address:      ContractAddress,
	Contract:     SettleLedgerPrecompile,
	Configurator: &configurator{},
}

type configurator struct{}

var _ contract.Configurator = (*configurator)(nil)

func init() {
	if err := modules.RegisterModule(Module); err != nil {
		panic(err)
	}
}

func (*configurator) MakeConfig() precompileconfig.Config { return new(Config) }

func (*configurator) Configure(
	_ precompileconfig.ChainConfig,
	cfg precompileconfig.Config,
	_ contract.StateDB,
	_ contract.ConfigurationBlockContext,
) error {
	if _, ok := cfg.(*Config); !ok {
		return fmt.Errorf("expected config type %T, got %T", &Config{}, cfg)
	}
	return nil
}
