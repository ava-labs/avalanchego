// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import (
	"fmt"
	"maps"
	"math/big"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/legacy"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	ethparams "github.com/ava-labs/libevm/params"
)

// invalidateDelegateTime is the Unix timestamp for August 2nd, 2025, midnight Eastern Time
// (August 2nd, 2025, 04:00 UTC)
const InvalidateDelegateUnix = 1754107200

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

// WarpMessages returns the storage keys of each Warp precompile entry in list,
// in order. Each entry is one Warp message, including entries without storage
// keys. It returns nil before Durango, when Warp is not active.
func (r RulesExtra) WarpMessages(list types.AccessList) []predicate.Predicate {
	if !r.IsDurango {
		return nil
	}
	return predicate.FromAccessList(list, warp.ContractAddress)
}

// AccessListGas computes the intrinsic gas for an access list. Once Warp is
// active, it calculates gas per-tuple, charging Warp predicates by
// [warp.PredicateGas]. Otherwise, it returns override=false to use the
// default calculation.
func (r RulesExtra) AccessListGas(accessList libevm.AccessList) (uint64, bool, error) {
	if !r.IsDurango {
		return 0, false, nil
	}
	gas, err := precompileconfig.AccessListGas(
		accessList,
		warp.ContractAddress,
		func(msg predicate.Predicate) (uint64, error) {
			return warp.PredicateGas(msg, cparams.Rules(r))
		},
	)
	return gas, true, err
}

var PrecompiledContractsApricotPhase2 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
}

var PrecompiledContractsApricotPhasePre6 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
}

var PrecompiledContractsApricotPhase6 = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.NativeAssetBalance{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.NativeAssetCall{}),
}

var PrecompiledContractsBanff = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
}

var PrecompiledContractsGranite = map[common.Address]vm.PrecompiledContract{
	nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
	nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
	P256VerifyAddress:                  &vm.P256Verify{},
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
	case r.IsBanff:
		return PrecompiledContractsBanff
	case r.IsApricotPhase6:
		return PrecompiledContractsApricotPhase6
	case r.IsApricotPhasePre6:
		return PrecompiledContractsApricotPhasePre6
	case r.IsApricotPhase2:
		return PrecompiledContractsApricotPhase2
	}
	return nil
}

// precompileOverrideBuiltin returns the precompile at addr that is scheduled
// by the active fork, such as the native asset precompiles and P256Verify.
func (r RulesExtra) precompileOverrideBuiltin(addr common.Address) (libevm.PrecompiledContract, bool) {
	precompile, ok := r.currentPrecompiles()[addr]
	return precompile, ok
}

func makePrecompile(contract contract.StatefulPrecompiledContract) libevm.PrecompiledContract {
	run := func(env vm.PrecompileEnvironment, input []byte, suppliedGas uint64) ([]byte, uint64, error) {
		rules := *GetRulesExtra(env.Rules())
		accessibleState := accessibleState{
			env: env,
			blockContext: &precompileBlockContext{
				number:           env.BlockNumber(),
				time:             env.BlockTime(),
				predicateResults: customtypes.PredicateResults(env.BlockHeader(), rules),
			},
		}

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

	// Warp is the only stateful precompile, and is active from Durango.
	if !r.IsDurango || addr != warp.ContractAddress {
		return nil, false
	}
	return makePrecompile(warp.WarpPrecompile), true
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

func (a accessibleState) GetRules() precompileconfig.Rules {
	chainConfigExtra := GetExtra(a.GetPrecompileEnv().ChainConfig())
	return chainConfigExtra.Rules(a.GetBlockContext().Timestamp())
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

func (p *precompileBlockContext) GetWarpMessageResults(txHash common.Hash) set.Bits {
	return p.predicateResults.Get(txHash, warp.ContractAddress)
}
