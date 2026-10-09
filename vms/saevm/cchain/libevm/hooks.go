// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package libevm

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/stateconf"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/libevm/extstate"
)

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
	rules := args.ChainConfig.Rules(args.BlockContext.BlockNumber, params.IsMergeTODO, args.BlockContext.Time)
	args.StateDB = wrapStateDB(rules, args.StateDB)

	if rules.IsShanghai {
		args.BlockContext.Random = new(common.Hash)
		args.BlockContext.Random.SetBytes(args.BlockContext.Difficulty.Bytes())
		args.BlockContext.Difficulty = new(big.Int)
	}

	return args
}

func (hooks) OverrideEVMResetArgs(rules params.Rules, args *vm.EVMResetArgs) *vm.EVMResetArgs {
	args.StateDB = wrapStateDB(rules, args.StateDB)
	return args
}

func wrapStateDB(rules params.Rules, statedb vm.StateDB) vm.StateDB {
	wrappedStateDB := extstate.New(statedb.(*state.StateDB))
	if params.GetRulesExtra(rules).IsApricotPhase1 {
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
