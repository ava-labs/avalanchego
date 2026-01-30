// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethclient

import (
	"context"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"

	// Force-load precompiles to trigger registration
	_ "github.com/ava-labs/avalanchego/graft/coreth/precompile/registry"

	"github.com/ava-labs/avalanchego/graft/coreth/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/coreth/interfaces"

	ethereum "github.com/ava-labs/libevm"
)

// Verify that [Client] implements required interfaces
var (
	_ bind.AcceptedContractCaller = (*Client)(nil)
	_ bind.ContractBackend        = (*Client)(nil)
	_ bind.ContractFilterer       = (*Client)(nil)
	_ bind.ContractTransactor     = (*Client)(nil)
	_ bind.DeployBackend          = (*Client)(nil)

	_ ethereum.ChainReader              = (*Client)(nil)
	_ ethereum.ChainStateReader         = (*Client)(nil)
	_ ethereum.TransactionReader        = (*Client)(nil)
	_ ethereum.TransactionSender        = (*Client)(nil)
	_ ethereum.ContractCaller           = (*Client)(nil)
	_ ethereum.GasEstimator             = (*Client)(nil)
	_ ethereum.GasPricer                = (*Client)(nil)
	_ ethereum.LogFilterer              = (*Client)(nil)
	_ interfaces.AcceptedStateReader    = (*Client)(nil)
	_ interfaces.AcceptedContractCaller = (*Client)(nil)
)

// SubscribeNewAcceptedTransactions subscribes to notifications about the accepted transaction hashes on the given channel.
func (ec *Client) SubscribeNewAcceptedTransactions(ctx context.Context, ch chan<- *common.Hash) (ethereum.Subscription, error) {
	sub, err := ec.c.EthSubscribe(ctx, ch, "newAcceptedTransactions")
	if err != nil {
		// Defensively prefer returning nil interface explicitly on error-path, instead
		// of letting default golang behavior wrap it with non-nil interface that stores
		// nil concrete type value.
		return nil, err
	}
	return sub, nil
}

// SubscribeNewPendingTransactions subscribes to notifications about the pending transaction hashes on the given channel.
func (ec *Client) SubscribeNewPendingTransactions(ctx context.Context, ch chan<- *common.Hash) (ethereum.Subscription, error) {
	sub, err := ec.c.EthSubscribe(ctx, ch, "newPendingTransactions")
	if err != nil {
		// Defensively prefer returning nil interface explicitly on error-path, instead
		// of letting default golang behavior wrap it with non-nil interface that stores
		// nil concrete type value.
		return nil, err
	}
	return sub, nil
}

// AcceptedCodeAt returns the contract code of the given account in the accepted state.
func (ec *Client) AcceptedCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return ec.CodeAt(ctx, account, nil)
}

// AcceptedNonceAt returns the account nonce of the given account in the accepted state.
// This is the nonce that should be used for the next transaction.
func (ec *Client) AcceptedNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return ec.NonceAt(ctx, account, nil)
}

// AcceptedCallContract executes a message call transaction in the accepted
// state.
func (ec *Client) AcceptedCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error) {
	return ec.CallContract(ctx, msg, nil)
}

// EstimateBaseFee tries to estimate the base fee for the next block if it were created
// immediately. There is no guarantee that this will be the base fee used in the next block
// or that the next base fee will be higher or lower than the returned value.
func (ec *Client) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	var hex hexutil.Big
	err := ec.c.CallContext(ctx, &hex, "eth_baseFee")
	if err != nil {
		return nil, err
	}
	return (*big.Int)(&hex), nil
}

func ToBlockNumArg(number *big.Int) string {
	return toBlockNumArg(number)
}
