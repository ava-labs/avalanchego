// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2015 The go-ethereum Authors
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

package bind

import (
	"context"
	"errors"
	"math/big"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/interfaces"
	"github.com/ethereum/go-ethereum/common"
)

var (
	// ErrNoCode is returned by call and transact operations for which the requested
	// recipient contract to operate on does not exist in the state db or does not
	// have any code associated with it (i.e. suicided).
	ErrNoCode = errors.New("no contract code at given address")

	// ErrNoAcceptedState is raised when attempting to perform a accepted state action
	// on a backend that doesn't implement AcceptedContractCaller.
	ErrNoAcceptedState = errors.New("backend does not support accepted state")

	// ErrNoCodeAfterDeploy is returned by WaitDeployed if contract creation leaves
	// an empty contract behind.
	ErrNoCodeAfterDeploy = errors.New("no contract code after deployment")
)

// ContractCaller defines the methods needed to allow operating with a contract on a read
// only basis.
type ContractCaller interface {
	// CodeAt returns the code of the given account. This is needed to differentiate
	// between contract internal errors and the local chain being out of sync.
	CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) ([]byte, error)

	// CallContract executes an Ethereum contract call with the specified data as the
	// input.
	CallContract(ctx context.Context, call interfaces.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// AcceptedContractCaller defines methods to perform contract calls on the pending state.
// Call will try to discover this interface when access to the accepted state is requested.
// If the backend does not support the pending state, Call returns ErrNoAcceptedState.
type AcceptedContractCaller interface {
	// AcceptedCodeAt returns the code of the given account in the accepted state.
	AcceptedCodeAt(ctx context.Context, contract common.Address) ([]byte, error)

	// AcceptedCallContract executes an Ethereum contract call against the accepted state.
	AcceptedCallContract(ctx context.Context, call interfaces.CallMsg) ([]byte, error)
}

// ContractTransactor defines the methods needed to allow operating with a contract
// on a write only basis. Besides the transacting method, the remainder are helpers
// used when the user does not provide some needed values, but rather leaves it up
// to the transactor to decide.
type ContractTransactor interface {
	// HeaderByNumber returns a block header from the current canonical chain. If
	// number is nil, the latest known header is returned.
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)

	// AcceptedCodeAt returns the code of the given account in the accepted state.
	AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error)

	// AcceptedNonceAt retrieves the current accepted nonce associated with an account.
	AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error)

	// SuggestGasPrice retrieves the currently suggested gas price to allow a timely
	// execution of a transaction.
	SuggestGasPrice(ctx context.Context) (*big.Int, error)

	// SuggestGasTipCap retrieves the currently suggested 1559 priority fee to allow
	// a timely execution of a transaction.
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)

	// EstimateGas tries to estimate the gas needed to execute a specific
	// transaction based on the current pending state of the backend blockchain.
	// There is no guarantee that this is the true gas limit requirement as other
	// transactions may be added or removed by miners, but it should provide a basis
	// for setting a reasonable default.
	EstimateGas(ctx context.Context, call interfaces.CallMsg) (gas uint64, err error)

	// SendTransaction injects the transaction into the pending pool for execution.
	SendTransaction(ctx context.Context, tx *types.Transaction) error
}

// ContractFilterer defines the methods needed to access log events using one-off
// queries or continuous event subscriptions.
type ContractFilterer interface {
	// FilterLogs executes a log filter operation, blocking during execution and
	// returning all the results in one batch.
	//
	// TODO(karalabe): Deprecate when the subscription one can return past data too.
	FilterLogs(ctx context.Context, query interfaces.FilterQuery) ([]types.Log, error)

	// SubscribeFilterLogs creates a background log filtering operation, returning
	// a subscription immediately, which can be used to stream the found events.
	SubscribeFilterLogs(ctx context.Context, query interfaces.FilterQuery, ch chan<- types.Log) (interfaces.Subscription, error)
}

// DeployBackend wraps the operations needed by WaitMined and WaitDeployed.
type DeployBackend interface {
	TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
}

// ContractBackend defines the methods needed to work with contracts on a read-write basis.
type ContractBackend interface {
	ContractCaller
	ContractTransactor
	ContractFilterer
}
