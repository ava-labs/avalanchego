// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/legacy"

	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/contracts/deployerallowlist"
	"github.com/ava-labs/subnet-evm/precompile/modules"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"

	ethparams "github.com/ava-labs/libevm/params"
)

// invalidateDelegateTime is the Unix timestamp for August 2nd, 2025, midnight Eastern Time
// (August 2nd, 2025, 04:00 UTC)
const InvalidateDelegateUnix = 1754107200

// P256VerifyAddress is the address of the p256 signature verification precompile
var P256VerifyAddress = common.BytesToAddress([]byte{0x1, 0x00})

type RulesExtra extras.Rules

func GetRulesExtra(r Rules) *extras.Rules {
	rules := payloads.Rules.GetPointer(&r)
	return (*extras.Rules)(rules)
}

func (r RulesExtra) CanCreateContract(ac *libevm.AddressContext, gas uint64, state libevm.StateReader) (uint64, error) {
	// If the allow list is enabled, check that [ac.Origin] has permission to deploy a contract.
	rules := (extras.Rules)(r)
	if rules.IsPrecompileEnabled(deployerallowlist.ContractAddress) {
		allowListRole := deployerallowlist.GetContractDeployerAllowListStatus(state, ac.Origin)
		if !allowListRole.IsEnabled() {
			gas = 0
			return gas, fmt.Errorf("tx.origin %s is not authorized to deploy a contract", ac.Origin)
		}
	}

	return gas, nil
}

func (RulesExtra) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	// TODO: Migrate call for txallowlist precompile to here from core/state_transition.go
	// when that is used from libevm.
	return nil
}

// MinimumGasConsumption is a no-op.
func (RulesExtra) MinimumGasConsumption(x uint64) uint64 {
	return (ethparams.NOOPHooks{}).MinimumGasConsumption(x)
}

var PrecompiledContractsGranite = map[common.Address]vm.PrecompiledContract{
	P256VerifyAddress: &vm.P256Verify{},
}

func (r RulesExtra) ActivePrecompiles(existing []common.Address) []common.Address {
	var addresses []common.Address
	addresses = slices.AppendSeq(addresses, maps.Keys(r.currentPrecompiles()))
	addresses = append(addresses, existing...)
	return addresses
}

func (r RulesExtra) currentPrecompiles() map[common.Address]vm.PrecompiledContract {
	switch {
	case r.IsGranite:
		return PrecompiledContractsGranite
	}
	return nil
}

// precompileOverrideBuiltin specifies precompiles that were activated prior to the
// dynamic precompile activation registry.
// These were only active historically and are not active in the current network.
func (r RulesExtra) precompileOverrideBuiltin(addr common.Address) (libevm.PrecompiledContract, bool) {
	precompile, ok := r.currentPrecompiles()[addr]
	return precompile, ok
}

func makePrecompile(contract contract.StatefulPrecompiledContract) libevm.PrecompiledContract {
	run := func(env vm.PrecompileEnvironment, input []byte, suppliedGas uint64) ([]byte, uint64, error) {
		header, err := env.BlockHeader()
		if err != nil {
			panic(err) // Should never happen
		}
		var predicateResults predicate.BlockResults
		if predicateResultsBytes := customheader.PredicateBytesFromExtra(header.Extra); len(predicateResultsBytes) > 0 {
			predicateResults, err = predicate.ParseBlockResults(predicateResultsBytes)
			if err != nil {
				panic(err) // Should never happen, as results are already validated in block validation
			}
		}
		accessibleState := accessibleState{
			env: env,
			blockContext: &precompileBlockContext{
				number:           env.BlockNumber(),
				time:             env.BlockTime(),
				predicateResults: predicateResults,
			},
		}

		rules := GetRulesExtra(env.Rules()).AvalancheRules
		switch call := env.IncomingCallType(); {
		case call != vm.DelegateCall && call != vm.CallCode: // Others always allowed
		case rules.IsGranite:
			return nil, 0, vm.ErrExecutionReverted
		case env.BlockTime() >= InvalidateDelegateUnix:
			env.InvalidateExecution(fmt.Errorf("precompile cannot be called with %s", call))
		default:
			// Otherwise, we allow the precompile to be called
		}

		return contract.Run(accessibleState, env.Addresses().EVMSemantic.Caller, env.Addresses().EVMSemantic.Self, input, suppliedGas, env.ReadOnly())
	}
	return vm.NewStatefulPrecompile(legacy.PrecompiledStatefulContract(run).Upgrade())
}

func (r RulesExtra) PrecompileOverride(addr common.Address) (libevm.PrecompiledContract, bool) {
	if p, ok := r.precompileOverrideBuiltin(addr); ok {
		return p, true
	}

	if _, ok := r.Precompiles[addr]; !ok {
		return nil, false
	}
	module, ok := modules.GetPrecompileModuleByAddress(addr)
	if !ok {
		return nil, false
	}

	return makePrecompile(module.Contract), true
}

type accessibleState struct {
	env          vm.PrecompileEnvironment
	blockContext *precompileBlockContext
}

func (a accessibleState) GetStateDB() contract.StateDB {
	// TODO the contracts should be refactored to call `env.ReadOnlyState`
	// or `env.StateDB` based on the env.ReadOnly() flag
	var state libevm.StateReader
	if a.env.ReadOnly() {
		state = a.env.ReadOnlyState()
	} else {
		state = a.env.StateDB()
	}
	return state.(contract.StateDB)
}

func (a accessibleState) GetBlockContext() contract.BlockContext {
	return a.blockContext
}

func (a accessibleState) GetChainConfig() precompileconfig.ChainConfig {
	return GetExtra(a.env.ChainConfig())
}

func (a accessibleState) GetRules() precompileconfig.Rules {
	chainConfigExtra := GetExtra(a.GetPrecompileEnv().ChainConfig())
	return chainConfigExtra.GetAvalancheRules(a.GetBlockContext().Timestamp())
}

func (a accessibleState) GetSnowContext() *snow.Context {
	return GetExtra(a.env.ChainConfig()).SnowCtx
}

func (a accessibleState) GetPrecompileEnv() vm.PrecompileEnvironment {
	return a.env
}

type precompileBlockContext struct {
	number           *big.Int
	time             uint64
	predicateResults predicate.BlockResults
}

func (p *precompileBlockContext) Number() *big.Int {
	return p.number
}

func (p *precompileBlockContext) Timestamp() uint64 {
	return p.time
}

func (p *precompileBlockContext) GetPredicateResults(txHash common.Hash, precompileAddress common.Address) set.Bits {
	return p.predicateResults.Get(txHash, precompileAddress)
}
