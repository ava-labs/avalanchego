// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/extstate"
)

// RegisterExtras registers the libevm hooks that wrap the EVM's StateDB in an
// [extstate.StateDB].
//
// Callers SHOULD NOT call it directly, and SHOULD instead use
// `vms/saevm/cchain/libevm.RegisterExtras()`, which registers all
// C-Chain extras together.
func RegisterExtras() {
	vm.RegisterHooks(hooks{})
}

// WithTempRegisteredExtras runs fn with the C-Chain's EVM hooks temporarily
// registered. Any previous hooks are restored after fn returns.
//
// Callers SHOULD NOT call it directly, and SHOULD instead use
// `vms/saevm/cchain/libevm.WithTempRegisteredExtras()`, which registers all
// C-Chain extras together.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	return vm.WithTempRegisteredHooks(lock, hooks{}, fn)
}

// hooks implements [vm.Hooks] to achieve the C-Chain's EVM behaviour.
type hooks struct{}

// PreprocessingGasCharge returns 0: the C-Chain charges predicate verification
// as part of a transaction's intrinsic gas, through the [params] hooks, rather
// than as a preprocessing charge.
func (hooks) PreprocessingGasCharge(common.Hash) (uint64, error) {
	return 0, nil
}

// OverrideNewEVMArgs is a hook that is called in [vm.NewEVM].
// It allows for the modification of the EVM arguments before the EVM is created.
// Specifically, we set Random to be the same as Difficulty since Shanghai.
// This allows using the same jump table as upstream.
// Then we set Difficulty to 0 as it is post Merge in upstream.
// Additionally we wrap the StateDB in an [extstate.StateDB].
func (hooks) OverrideNewEVMArgs(args *vm.NewEVMArgs) *vm.NewEVMArgs {
	args.StateDB = wrapStateDB(args.StateDB)

	rules := args.ChainConfig.Rules(args.BlockContext.BlockNumber, true /*isMerge*/, args.BlockContext.Time)
	if rules.IsShanghai {
		args.BlockContext.Random = new(common.Hash)
		args.BlockContext.Random.SetBytes(args.BlockContext.Difficulty.Bytes())
		args.BlockContext.Difficulty = new(big.Int)
	}

	return args
}

func (hooks) OverrideEVMResetArgs(_ params.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(args.StateDB)
	return args
}

func wrapStateDB(statedb vm.StateDB) vm.StateDB {
	return extstate.New(statedb.(*state.StateDB))
}
