// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Defines the interface for the configuration and execution of a precompile contract
package contract

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// StatefulPrecompiledContract is the interface for executing a precompiled contract
type StatefulPrecompiledContract interface {
	// Run executes the precompiled contract.
	Run(env Environment, input []byte) ([]byte, error)
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
	// GetWarpMessage returns the index-th Warp message in the current
	// transaction's access list. It returns false if there is no such message.
	GetWarpMessage(index int) (predicate.Predicate, bool)

	TxHash() common.Hash

	Snapshot() int
	RevertToSnapshot(int)
}

// Environment wraps [vm.PrecompileEnvironment] with Avalanche-specific details.
type Environment interface {
	vm.PrecompileEnvironment
	Caller() common.Address
	SnowContext() *snow.Context
	// ExtraRules returns any registered rules on [params.Rules].
	ExtraRules() precompileconfig.Rules
	// WarpMessageResults returns the result of verifying the Warp messages
	// of the given transaction.
	WarpMessageResults(txHash common.Hash) set.Bits
}
