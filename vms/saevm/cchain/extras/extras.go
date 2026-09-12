// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package extras registers the libevm hooks and extras that configure libevm
// for C-Chain behaviour.
package extras

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
)

// RegisterLibEVM registers the C-Chain EVM hooks along with
// [customtypes.Register], [extstate.RegisterExtras], and
// [corethparams.RegisterExtras]. Together these are necessary and sufficient
// for configuring libevm for C-Chain behaviour.
//
// It MUST NOT be called more than once and therefore is only allowed to be used
// in tests and `package main`, to avoid polluting other packages that
// transitively depend on this one but don't need registration.
func RegisterLibEVM() {
	vm.RegisterHooks(hooks{})
	customtypes.Register()
	extstate.RegisterExtras()
	corethparams.RegisterExtras()
}

// WithTempRegisteredLibEVM runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterLibEVM], but limited to the life of `fn`.
func WithTempRegisteredLibEVM(fn func() error) error {
	return libevm.WithTemporaryExtrasLock(func(lock libevm.ExtrasLock) error {
		for _, wrap := range []func(libevm.ExtrasLock, func() error) error{
			withTempRegisteredHooks,
			customtypes.WithTempRegisteredExtras,
			extstate.WithTempRegisteredExtras,
			corethparams.WithTempRegisteredExtras,
		} {
			inner := fn
			fn = func() error { return wrap(lock, inner) }
		}
		return fn()
	})
}

func withTempRegisteredHooks(lock libevm.ExtrasLock, fn func() error) error {
	return vm.WithTempRegisteredHooks(lock, hooks{}, fn)
}

var _ vm.Hooks = hooks{}

// hooks implements the C-Chain overrides of libevm EVM construction.
type hooks struct{}

// PreprocessingGasCharge reports no charge. The C-Chain registers no
// preprocessing precompiles, so this only satisfies [vm.Hooks].
func (hooks) PreprocessingGasCharge(common.Hash) (uint64, error) {
	return 0, nil
}

// OverrideNewEVMArgs wraps the state database as [hooks.OverrideEVMResetArgs]
// does and, from Shanghai, sets Random to Difficulty and zeroes Difficulty.
// Upstream treats Shanghai as post-merge, so this lets the C-Chain reuse the
// upstream jump table unchanged.
func (hooks) OverrideNewEVMArgs(args *vm.NewEVMArgs) *vm.NewEVMArgs {
	rules := args.ChainConfig.Rules(args.BlockContext.BlockNumber, corethparams.IsMergeTODO, args.BlockContext.Time)
	args.StateDB = wrapStateDB(rules, args.StateDB)

	if rules.IsShanghai {
		args.BlockContext.Random = new(common.Hash)
		args.BlockContext.Random.SetBytes(args.BlockContext.Difficulty.Bytes())
		args.BlockContext.Difficulty = new(big.Int)
	}

	return args
}

// OverrideEVMResetArgs wraps the state database in an [extstate.StateDB],
// with the pre-AP1 committed-state behaviour of [stateDBAP0] when the rules
// predate AP1.
func (hooks) OverrideEVMResetArgs(rules params.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(rules, args.StateDB)
	return args
}

func wrapStateDB(rules params.Rules, statedb vm.StateDB) vm.StateDB {
	wrappedStateDB := extstate.New(statedb.(*state.StateDB))
	if corethparams.GetRulesExtra(rules).IsApricotPhase1 {
		return wrappedStateDB
	}
	return &stateDBAP0{wrappedStateDB}
}

// stateDBAP0 implements the GetCommittedState behavior that existed prior to
// the AP1 upgrade.
//
// Since launch, state keys have been normalized to allow for multicoin
// balances. However, at launch GetCommittedState was not updated. This meant
// that gas refunds were not calculated as expected for SSTORE opcodes.
//
// This oversight was fixed in AP1, but in order to execute blocks prior to AP1
// and generate the same merkle root, this behavior must be maintained.
//
// See the [extstate] package for details around state key normalization.
type stateDBAP0 struct {
	*extstate.StateDB
}

func (s *stateDBAP0) GetCommittedState(addr common.Address, key common.Hash, _ ...stateconf.StateDBStateOption) common.Hash {
	return s.StateDB.GetCommittedState(addr, key, stateconf.SkipStateKeyTransformation())
}
