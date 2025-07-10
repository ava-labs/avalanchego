// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Defines the interface for the configuration and execution of a precompile contract
package contract

import (
	"math/big"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/libevm/common"
	ethtypes "github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/holiman/uint256"
)

// StatefulPrecompiledContract is the interface for executing a precompiled contract
type StatefulPrecompiledContract interface {
	// Run executes the precompiled contract.
	Run(accessibleState AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error)
}

// StateDB is the interface for accessing EVM state
type StateDB interface {
	GetState(common.Address, common.Hash) common.Hash
	SetState(common.Address, common.Hash, common.Hash)

	SetNonce(common.Address, uint64)
	GetNonce(common.Address) uint64

	GetBalance(common.Address) *uint256.Int
	AddBalance(common.Address, *uint256.Int)
	GetBalanceMultiCoin(common.Address, common.Hash) *big.Int
	AddBalanceMultiCoin(common.Address, common.Hash, *big.Int)
	SubBalanceMultiCoin(common.Address, common.Hash, *big.Int)

	CreateAccount(common.Address)
	Exist(common.Address) bool

	AddLog(*ethtypes.Log)
	Logs() []*ethtypes.Log
	GetPredicateStorageSlots(address common.Address, index int) ([]byte, bool)

	GetTxHash() common.Hash

	Snapshot() int
	RevertToSnapshot(int)
}

// AccessibleState defines the interface exposed to stateful precompile contracts
type AccessibleState interface {
	GetStateDB() StateDB
	GetBlockContext() BlockContext
	GetSnowContext() *snow.Context
	GetChainConfig() precompileconfig.ChainConfig
	GetPrecompileEnv() vm.PrecompileEnvironment
}

// ConfigurationBlockContext defines the interface required to configure a precompile.
type ConfigurationBlockContext interface {
	Number() *big.Int
	Timestamp() uint64
}

type BlockContext interface {
	ConfigurationBlockContext
	// GetPredicateResults returns the result of verifying the predicates of the
	// given transaction, precompile address pair as a byte array.
	GetPredicateResults(txHash common.Hash, precompileAddress common.Address) []byte
}

type Configurator interface {
	MakeConfig() precompileconfig.Config
	Configure(
		chainConfig precompileconfig.ChainConfig,
		precompileconfig precompileconfig.Config,
		state StateDB,
		blockContext ConfigurationBlockContext,
	) error
}
