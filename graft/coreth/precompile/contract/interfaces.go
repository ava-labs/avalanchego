// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Defines the interface for the configuration and execution of a precompile contract
package contract

import (
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// StatefulPrecompiledContract is the interface for executing a precompiled contract
type StatefulPrecompiledContract interface {
	// Run executes the precompiled contract.
	Run(accessibleState AccessibleState, caller common.Address, addr common.Address, input []byte, readOnly bool) ([]byte, error)
}

// StateDB is the interface for accessing EVM state
type StateDB interface {
	GetAssetBalance(common.Address, common.Hash) *uint256.Int
	AddAssetBalance(common.Address, common.Hash, *uint256.Int)
	SubAssetBalance(common.Address, common.Hash, *uint256.Int)

	CreateAccount(common.Address)
	Exist(common.Address) bool

	AddLog(*ethtypes.Log)
	Logs() []*ethtypes.Log
	GetPredicate(address common.Address, index int) (predicate.Predicate, bool)

	TxHash() common.Hash

	Snapshot() int
	RevertToSnapshot(int)
}

// AccessibleState defines the interface exposed to stateful precompile contracts
type AccessibleState interface {
	GetStateDB() StateDB
	GetBlockContext() BlockContext
	GetSnowContext() *snow.Context
	GetPrecompileEnv() vm.PrecompileEnvironment
	GetRules() precompileconfig.Rules
}

// ConfigurationBlockContext defines the interface required to configure a precompile.
type ConfigurationBlockContext interface {
	Number() *big.Int
	Timestamp() uint64
}

type BlockContext interface {
	ConfigurationBlockContext
	// GetPredicateResults returns the result of verifying the predicates of the
	// given transaction, precompile address pair.
	GetPredicateResults(txHash common.Hash, precompileAddress common.Address) set.Bits
}
