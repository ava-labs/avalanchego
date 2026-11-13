// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"

	ethparams "github.com/ava-labs/libevm/params"
)

var _ ethparams.RulesHooks = Rules{}

// Rules defines the activation status of various Avalanche network
// upgrades.
type Rules struct {
	IsApricotPhase1    bool
	IsApricotPhase2    bool
	IsApricotPhasePre6 bool
	IsApricotPhase6    bool
	IsBanff            bool
	IsDurango          bool
	IsFortuna          bool
	IsGranite          bool
	IsHelicon          bool
	IsIgloo            bool
}

func (Rules) CanCreateContract(_ *libevm.AddressContext, gas uint64, _ libevm.StateReader) (uint64, error) {
	return gas, nil
}

func (r Rules) ShouldRefundGas() bool {
	return !r.IsApricotPhase1
}

// MinimumGasConsumption returns the ACP-194 gas-charged floor (ceil(limit/2)).
func (r Rules) MinimumGasConsumption(limit uint64) uint64 {
	if r.IsHelicon {
		return hook.MinimumGasConsumption(limit)
	}
	return (ethparams.NOOPHooks{}).MinimumGasConsumption(limit)
}

// ShouldCreditBaseFeeToCoinbase returns true to credit the base fee to the
// coinbase (the blackhole address on the C-Chain). The C-Chain has
// historically credited the full fee (base + priority) to the blackhole
// address, but by default libevm's state transition only credits the priority
// fee and discards the base fee.
func (Rules) ShouldCreditBaseFeeToCoinbase() bool {
	return true
}

// p256VerifyAddress is the address of the p256 signature verification precompile
var p256VerifyAddress = common.BytesToAddress([]byte{0x1, 0x00})

// AccessListGas computes the intrinsic gas for an access list. Once Warp is
// active, it calculates gas per-tuple, charging Warp messages by
// [warp.GasConfig.MessageGas]. Otherwise, it returns override=false to use
// the default calculation.
func (r Rules) AccessListGas(accessList libevm.AccessList) (uint64, bool, error) {
	if !r.IsDurango {
		return 0, false, nil
	}
	warpGas := warp.PreGraniteGasConfig
	if r.IsGranite {
		warpGas = warp.GraniteGasConfig
	}
	gas, err := message.AccessListGas(accessList, warpGas.MessageGas)
	return gas, true, err
}

// ActivePrecompiles returns the list of precompile addresses that are warmed
// under EIP-2929.
func (r Rules) ActivePrecompiles(existing []common.Address) []common.Address {
	addresses := slices.Clone(existing)
	switch {
	case r.IsGranite:
		addresses = append(addresses, p256VerifyAddress)
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

func (r Rules) PrecompileOverride(addr common.Address) (libevm.PrecompiledContract, bool) {
	precompile, ok := r.currentPrecompiles()[addr]
	return precompile, ok
}

func (r Rules) currentPrecompiles() map[common.Address]vm.PrecompiledContract {
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

var (
	precompiledContractsApricotPhase2 = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
	}

	precompiledContractsApricotPhasePre6 = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	}

	precompiledContractsApricotPhase6 = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
	}

	precompiledContractsBanff = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	}

	precompiledContractsDurango = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		message.ContractAddress:            makePrecompile(warp.NewPrecompile(warp.PreGraniteGasConfig)),
	}

	precompiledContractsGranite = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		message.ContractAddress:            makePrecompile(warp.NewPrecompile(warp.GraniteGasConfig)),
		p256VerifyAddress:                  &vm.P256Verify{},
	}
)

// makePrecompile adapts a [contract.StatefulPrecompiledContract] to a [libevm.PrecompiledContract]
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
	warpResults message.BlockResults
}

func newEnvironment(env vm.PrecompileEnvironment) (*environment, error) {
	rules := GetRulesExtra(env.Rules())

	if call := env.IncomingCallType(); (call == vm.DelegateCall || call == vm.CallCode) && rules.IsGranite {
		env.UseGas(env.Gas()) // A revert would otherwise refund the remaining gas.
		return nil, vm.ErrExecutionReverted
	}

	predicateBytes := predicateBytesFromExtra(*rules, env.BlockHeader().Extra)

	return &environment{
		PrecompileEnvironment: env,
		warpResults:           customtypes.PredicateResults(env.BlockHeader(), predicateBytes),
	}, nil
}

func (e *environment) Caller() common.Address {
	return e.Addresses().EVMSemantic.Caller
}

func (e *environment) SnowContext() *snow.Context {
	return GetExtra(e.ChainConfig()).SnowCtx
}

func (e *environment) WarpMessageResults(txHash common.Hash) set.Bits {
	return e.warpResults.Get(txHash)
}
