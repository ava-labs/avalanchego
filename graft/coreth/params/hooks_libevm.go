// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/cchain/cparams"

	ethparams "github.com/ava-labs/libevm/params"
)

// P256VerifyAddress is the address of the p256 signature verification precompile
var P256VerifyAddress = common.BytesToAddress([]byte{0x1, 0x00})

type RulesExtra cparams.Rules // TODO(alarso16): Avoid new type.

func GetRulesExtra(r ethparams.Rules) *cparams.Rules {
	rules := payloads.Rules.GetPointer(&r)
	return (*cparams.Rules)(rules)
}

func (RulesExtra) CanCreateContract(_ *libevm.AddressContext, gas uint64, _ libevm.StateReader) (uint64, error) {
	return gas, nil
}

func (RulesExtra) CanExecuteTransaction(_ common.Address, _ *common.Address, _ libevm.StateReader) error {
	return nil
}

func (r RulesExtra) ShouldRefundGas() bool {
	return !r.IsApricotPhase1
}

// MinimumGasConsumption returns the ACP-194 gas-charged floor (ceil(limit/2)).
func (r RulesExtra) MinimumGasConsumption(limit uint64) uint64 {
	if cparams.Rules(r).IsHelicon {
		return hook.MinimumGasConsumption(limit)
	}
	return (ethparams.NOOPHooks{}).MinimumGasConsumption(limit)
}

// ShouldCreditBaseFeeToCoinbase returns true to credit the base fee to the
// coinbase (the blackhole address on the C-Chain). The C-Chain has
// historically credited the full fee (base + priority) to the blackhole
// address, but by default libevm's state transition only credits the priority
// fee and discards the base fee.
func (RulesExtra) ShouldCreditBaseFeeToCoinbase() bool {
	return true
}

// warpPredicaters charges for Warp predicates, which are the only predicates.
//
// TODO(JonathanOppenheimer): Charge Warp messages directly once the shared
// predicate gas helper is replaced.
var warpPredicaters = map[common.Address]precompileconfig.Predicater{
	warp.ContractAddress: &warp.Config{},
}

// AccessListGas computes the intrinsic gas for an access list. Once Warp is
// active, it calculates gas per-tuple, charging Warp predicates by
// [warp.Config.PredicateGas]. Otherwise, it returns override=false to use the
// default calculation.
func (r RulesExtra) AccessListGas(accessList libevm.AccessList) (uint64, bool, error) {
	if !r.IsDurango {
		return 0, false, nil
	}
	gas, err := precompileconfig.AccessListGasWithPredicates(cparams.Rules(r), warpPredicaters, accessList)
	return gas, true, err
}

var precompiledContractsApricotPhase2 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
}

var precompiledContractsApricotPhasePre6 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
}

var precompiledContractsApricotPhase6 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
}

var precompiledContractsBanff = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
}

var precompiledContractsDurango = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	warp.ContractAddress:               makePrecompile(warp.WarpPrecompile),
}

var precompiledContractsGranite = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	warp.ContractAddress:               makePrecompile(warp.WarpPrecompile),
	P256VerifyAddress:                  &vm.P256Verify{},
}

// ActivePrecompiles returns the list of precompile addresses that are warmed
// under EIP-2929.
func (r RulesExtra) ActivePrecompiles(existing []common.Address) []common.Address {
	addresses := slices.Clone(existing)
	switch {
	case r.IsGranite:
		addresses = append(addresses, P256VerifyAddress)
		fallthrough
	case r.IsApricotPhase2:
		addresses = append(addresses,
			nativeasset.GenesisContractAddr,
			nativeasset.NativeAssetBalanceAddr,
			nativeasset.NativeAssetCallAddr,
		)
	}
	return addresses
}

func (r RulesExtra) PrecompileOverride(addr common.Address) (libevm.PrecompiledContract, bool) {
	precompile, ok := r.currentPrecompiles()[addr]
	return precompile, ok
}

func (r RulesExtra) currentPrecompiles() map[common.Address]vm.PrecompiledContract {
	switch {
	case r.IsGranite:
		return precompiledContractsGranite
	case r.IsDurango:
		return precompiledContractsDurango
	case r.IsBanff:
		return precompiledContractsBanff
	case r.IsApricotPhase6:
		return precompiledContractsApricotPhase6
	case r.IsApricotPhasePre6:
		return precompiledContractsApricotPhasePre6
	case r.IsApricotPhase2:
		return precompiledContractsApricotPhase2
	}
	return nil
}

// makePrecompile adapts a [contract.StatefulPrecompiledContract] to a [libevm.PrecompiledContract].
func makePrecompile(contract contract.StatefulPrecompiledContract) libevm.PrecompiledContract {
	return vm.NewStatefulPrecompile(func(vmEnv vm.PrecompileEnvironment, input []byte) ([]byte, error) {
		contractEnv, err := newEnvironment(vmEnv)
		if err != nil {
			return nil, err
		}
		return contract.Run(contractEnv, input)
	})
}

var _ contract.Environment = (*environment)(nil)

type environment struct {
	vm.PrecompileEnvironment
	predicateResults predicate.BlockResults
	rules            *cparams.Rules
}

func newEnvironment(env vm.PrecompileEnvironment) (*environment, error) {
	rules := GetRulesExtra(env.Rules())

	if call := env.IncomingCallType(); (call == vm.DelegateCall || call == vm.CallCode) && rules.IsGranite {
		env.UseGas(env.Gas()) // A revert would otherwise refund the remaining gas.
		return nil, vm.ErrExecutionReverted
	}

	return &environment{
		PrecompileEnvironment: env,
		predicateResults:      customtypes.PredicateResults(env.BlockHeader(), *rules),
		rules:                 rules,
	}, nil
}

func (e *environment) Caller() common.Address {
	return e.Addresses().EVMSemantic.Caller
}

func (e *environment) ExtraRules() precompileconfig.Rules {
	return e.rules
}

func (e *environment) SnowContext() *snow.Context {
	return GetExtra(e.ChainConfig()).SnowCtx
}

func (e *environment) WarpMessageResults(txHash common.Hash) set.Bits {
	return e.predicateResults.Get(txHash, warp.ContractAddress)
}
