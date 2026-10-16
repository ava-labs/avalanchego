// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/stateconf"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/cparams"

	ethparams "github.com/ava-labs/libevm/params"
)

// RegisterExtras registers hooks with libevm to achieve Avalanche behaviour of
// the EVM. It MUST NOT be called more than once and therefore is only allowed
// to be used in tests and `package main`, to avoid polluting other packages
// that transitively depend on this one but don't need registration.
func RegisterExtras() {
	// Although the registration function refers to just Hooks (not Extras) this
	// will be changed in the future to standardise across libevm, hence the
	// name of the function we're in.
	vm.RegisterHooks(hooks{})
}

// WithTempRegisteredExtras runs `fn` with temporary registration otherwise
// equivalent to a call to [RegisterExtras], but limited to the life of `fn`.
//
// This function is not intended for direct use. Use
// `evm.WithTempRegisteredLibEVMExtras()` instead as it calls this along with
// all other temporary-registration functions.
func WithTempRegisteredExtras(lock libevm.ExtrasLock, fn func() error) error {
	return vm.WithTempRegisteredHooks(lock, hooks{}, fn)
}

type hooks struct{}

// PreprocessingGasCharge is not necessary.
// It is required to implement the vm.Hooks interface, but is only needed post-SAE.
func (hooks) PreprocessingGasCharge(common.Hash) (uint64, error) {
	return 0, nil
}

// OverrideNewEVMArgs is a hook that is called in [vm.NewEVM].
// It allows for the modification of the EVM arguments before the EVM is created.
// Specifically, we set Random to be the same as Difficulty since Shanghai.
// This allows using the same jump table as upstream.
// Then we set Difficulty to 0 as it is post Merge in upstream.
// Additionally we wrap the StateDB with the appropriate StateDB wrapper,
// which is used in coreth to process historical pre-AP1 blocks with the
// [StateDbAP1.GetCommittedState] method as it was historically.
func (hooks) OverrideNewEVMArgs(args *vm.NewEVMArgs) *vm.NewEVMArgs {
	rules := args.ChainConfig.Rules(args.BlockContext.BlockNumber, true /*isMerge*/, args.BlockContext.Time)
	args.StateDB = wrapStateDB(rules, args.StateDB)

	if rules.IsShanghai {
		args.BlockContext.Random = new(common.Hash)
		args.BlockContext.Random.SetBytes(args.BlockContext.Difficulty.Bytes())
		args.BlockContext.Difficulty = new(big.Int)
	}

	return args
}

func (hooks) OverrideEVMResetArgs(rules ethparams.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(rules, args.StateDB)
	return args
}

func wrapStateDB(rules ethparams.Rules, statedb vm.StateDB) vm.StateDB {
	wrappedStateDB := extstate.New(statedb.(*state.StateDB))
	if cparams.GetRulesExtra(rules).IsApricotPhase1 {
		return wrappedStateDB
	}
	return &StateDBAP0{wrappedStateDB}
}

// StateDBAP0 implements the GetCommittedState behavior that existed prior to
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
type StateDBAP0 struct {
	*extstate.StateDB
}

func (s *StateDBAP0) GetCommittedState(addr common.Address, key common.Hash, _ ...stateconf.StateDBStateOption) common.Hash {
	return s.StateDB.GetCommittedState(addr, key, stateconf.SkipStateKeyTransformation())
}
