// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cparams

import (
	"fmt"
	"iter"
	"maps"
	"math/big"
	"slices"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/legacy"

	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/modules"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	ethparams "github.com/ava-labs/libevm/params"
)

// AvalancheRules defines the activation status of various Avalanche network
// upgrades.
type AvalancheRules struct {
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

var _ precompileconfig.Rules = AvalancheRules{}

func (a AvalancheRules) IsGraniteActivated() bool {
	return a.IsGranite
}

func (a AvalancheRules) IsDurangoActivated() bool {
	return a.IsDurango
}

// RulesExtra is an extension of AvalancheRules that includes additional
// precompile-related configurations.
type RulesExtra struct {
	AvalancheRules

	// Predicaters maps addresses to stateful precompile Predicaters
	// that are enabled for this rule set.
	Predicaters map[common.Address]precompileconfig.Predicater

	// precompiles maps addresses to stateful precompiled contracts that are enabled
	// for this rule set.
	// Note: none of these addresses should conflict with the address space used by
	// any existing precompiles.
	precompiles map[common.Address]precompileconfig.Config
}

// constructRulesExtra acts as an adjunct to the [params.ChainConfig.Rules]
// method. Its primary purpose is to construct the extra payload for the
// [params.Rules] but it MAY also modify the [params.Rules].
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func constructRulesExtra(c *ethparams.ChainConfig, r *ethparams.Rules, cEx *ChainConfig, blockNum *big.Int, isMerge bool, timestamp uint64) RulesExtra {
	var rules RulesExtra
	if cEx == nil {
		return rules
	}

	rules.AvalancheRules = cEx.Rules(timestamp)
	rules.Predicaters = make(map[common.Address]precompileconfig.Predicater)
	rules.precompiles = make(map[common.Address]precompileconfig.Config)
	for addr, config := range getActiveConfigs(cEx.PrecompileUpgrades, timestamp) {
		rules.precompiles[addr] = config
		if predicater, ok := config.(precompileconfig.Predicater); ok {
			rules.Predicaters[addr] = predicater
		}
	}

	return rules
}

func getActiveConfigs(upgrades []PrecompileUpgrade, timestamp uint64) iter.Seq2[common.Address, precompileconfig.Config] {
	return func(yield func(common.Address, precompileconfig.Config) bool) {
		for _, module := range modules.RegisteredModules() {
			key := module.ConfigKey

			// Return most recent config from list.
			// Any disabled config will not be returned.
			for _, upgrade := range slices.Backward(upgrades) {
				if upgrade.Key() != key {
					continue
				}

				if !isTimestampForked(upgrade.Timestamp(), timestamp) {
					continue
				}

				cfg := upgrade.Config
				if cfg.IsDisabled() {
					break
				}

				if !yield(module.Address, cfg) {
					return
				}

				break
			}
		}
	}
}

var _ predicate.Predicates = RulesExtra{}

func (r RulesExtra) HasPredicate(addr common.Address) bool {
	_, ok := r.Predicaters[addr]
	return ok
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
func (RulesExtra) ShouldCreditBaseFeeToCoinbase() bool {
	return true
}

// AccessListGas computes the intrinsic gas for an access list.
// When predicaters exist, it calculates gas per-tuple, delegating to predicate
// contracts for addresses that have them. Otherwise, it returns override=false
// to use the default calculation.
func (r RulesExtra) AccessListGas(accessList libevm.AccessList) (uint64, bool, error) {
	if len(r.Predicaters) == 0 {
		return 0, false, nil
	}
	gas, err := evmprecompileconfig.AccessListGasWithPredicates(r.AvalancheRules, r.Predicaters, accessList)
	return gas, true, err
}

var (
	p256VerifyAddress = common.BytesToAddress([]byte{0x1, 0x00})

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

	precompiledContractsGranite = map[common.Address]vm.PrecompiledContract{
		nativeasset.GenesisContractAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetBalanceAddr: makePrecompile(&nativeasset.DeprecatedContract{}),
		nativeasset.NativeAssetCallAddr:    makePrecompile(&nativeasset.DeprecatedContract{}),
		p256VerifyAddress:                  &vm.P256Verify{},
	}
)

func (r RulesExtra) ActivePrecompiles(existing []common.Address) []common.Address {
	var addresses []common.Address
	addresses = slices.AppendSeq(addresses, maps.Keys(r.currentPrecompiles()))
	addresses = append(addresses, existing...)
	return addresses
}

func (r RulesExtra) currentPrecompiles() map[common.Address]vm.PrecompiledContract {
	switch {
	case r.IsGranite:
		return precompiledContractsGranite
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

// precompileOverrideBuiltin specifies precompiles that were activated prior to the
// dynamic precompile activation registry.
// These were only active historically and are not active in the current network.
func (r RulesExtra) precompileOverrideBuiltin(addr common.Address) (libevm.PrecompiledContract, bool) {
	precompile, ok := r.currentPrecompiles()[addr]
	return precompile, ok
}

const invalidateDelegateUnix = 1754107200 // August 2nd, 2025, 04:00 UTC

func makePrecompile(contract contract.StatefulPrecompiledContract) libevm.PrecompiledContract {
	run := func(env vm.PrecompileEnvironment, input []byte, suppliedGas uint64) ([]byte, uint64, error) {
		header, err := env.BlockHeader()
		if err != nil {
			panic(err) // Should never happen
		}
		var predicateResults predicate.BlockResults
		rules := GetRulesExtra(env.Rules()).AvalancheRules
		if predicateResultsBytes := predicateBytesFromExtra(rules, header.Extra); len(predicateResultsBytes) > 0 {
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

		switch call := env.IncomingCallType(); {
		case call != vm.DelegateCall && call != vm.CallCode: // Others always allowed
		case rules.IsGranite:
			return nil, 0, vm.ErrExecutionReverted
		case env.BlockTime() >= invalidateDelegateUnix:
			env.InvalidateExecution(fmt.Errorf("precompile cannot be called with %s", call))
		default:
			// Otherwise, we allow the precompile to be called
		}

		return contract.Run(accessibleState, env.Addresses().EVMSemantic.Caller, env.Addresses().EVMSemantic.Self, input, suppliedGas, env.ReadOnly())
	}
	return vm.NewStatefulPrecompile(legacy.PrecompiledStatefulContract(run).Upgrade())
}

// predicateBytesFromExtra returns the predicate result bytes from the header's
// extra data. If the extra data is not long enough, an empty slice is returned.
func predicateBytesFromExtra(rules AvalancheRules, extra []byte) []byte {
	offset := predicateBytesOffset(rules)
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to `offset`.
	// After Durango, the VM pre-verifies the extra data past `offset` is valid.
	if len(extra) <= offset {
		return nil
	}
	return extra[offset:]
}

func predicateBytesOffset(rules AvalancheRules) int {
	switch {
	case rules.IsHelicon:
		return 0
	case rules.IsFortuna:
		return acp176.StateSize
	default:
		return ap3.WindowSize
	}
}

func (r RulesExtra) PrecompileOverride(addr common.Address) (libevm.PrecompiledContract, bool) {
	if p, ok := r.precompileOverrideBuiltin(addr); ok {
		return p, true
	}

	if _, ok := r.precompiles[addr]; !ok {
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

func (p *precompileBlockContext) GetPredicateResults(txHash common.Hash, precompileAddress common.Address) set.Bits {
	return p.predicateResults.Get(txHash, precompileAddress)
}
