// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevmextras

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/params"
)

var _ vm.Hooks = vmHooks{}

type vmHooks struct{}

// PreprocessingGasCharge returns 0. The C-Chain charges predicate verification
// as part of a transaction's intrinsic gas.
func (vmHooks) PreprocessingGasCharge(common.Hash) (uint64, error) {
	return 0, nil
}

// OverrideNewEVMArgs is a hook that is called in [vm.NewEVM].
func (vmHooks) OverrideNewEVMArgs(args *vm.NewEVMArgs) *vm.NewEVMArgs {
	args.StateDB = wrapStateDB(args.StateDB)

	bc := &args.BlockContext
	if args.ChainConfig.IsShanghai(bc.BlockNumber, bc.Time) {
		bc.Random = new(common.BigToHash(bc.Difficulty))
		bc.Difficulty = new(big.Int)
	}

	return args
}

func (vmHooks) OverrideEVMResetArgs(_ params.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(args.StateDB)
	return args
}

func wrapStateDB(statedb vm.StateDB) vm.StateDB {
	return NewStateDB(statedb.(*state.StateDB))
}
