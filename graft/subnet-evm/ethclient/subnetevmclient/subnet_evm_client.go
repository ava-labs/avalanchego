// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
// Copyright 2021 The go-ethereum Authors
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

// Package subnetevmclient provides an RPC client for subnet-evm-specific APIs.
package subnetevmclient

import (
	"context"
	"math/big"
	"runtime"
	"runtime/debug"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/ethclient"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/rpc"
	ethereum "github.com/ava-labs/libevm"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
)

// Client is a wrapper around rpc.Client that implements geth-specific functionality.
//
// If you want to use the standardized Ethereum RPC functionality, use ethclient.Client instead.
type Client struct {
	c *rpc.Client
}

// New creates a client that uses the given RPC client.
func New(c *rpc.Client) *Client {
	return &Client{c}
}

// CreateAccessList tries to create an access list for a specific transaction based on the
// current pending state of the blockchain.
func (ec *Client) CreateAccessList(ctx context.Context, msg ethereum.CallMsg) (*types.AccessList, uint64, string, error) {
	type accessListResult struct {
		Accesslist *types.AccessList `json:"accessList"`
		Error      string            `json:"error,omitempty"`
		GasUsed    hexutil.Uint64    `json:"gasUsed"`
	}
	var result accessListResult
	if err := ec.c.CallContext(ctx, &result, "eth_createAccessList", toCallArg(msg)); err != nil {
		return nil, 0, "", err
	}
	return result.Accesslist, uint64(result.GasUsed), result.Error, nil
}

// AccountResult is the result of a GetProof operation.
type AccountResult struct {
	Address      common.Address  `json:"address"`
	AccountProof []string        `json:"accountProof"`
	Balance      *big.Int        `json:"balance"`
	CodeHash     common.Hash     `json:"codeHash"`
	Nonce        uint64          `json:"nonce"`
	StorageHash  common.Hash     `json:"storageHash"`
	StorageProof []StorageResult `json:"storageProof"`
}

// StorageResult provides a proof for a key-value pair.
type StorageResult struct {
	Key   string   `json:"key"`
	Value *big.Int `json:"value"`
	Proof []string `json:"proof"`
}

// GetProof returns the account and storage values of the specified account including the Merkle-proof.
// The block number can be nil, in which case the value is taken from the latest known block.
func (ec *Client) GetProof(ctx context.Context, account common.Address, keys []string, blockNumber *big.Int) (*AccountResult, error) {
	type storageResult struct {
		Key   string       `json:"key"`
		Value *hexutil.Big `json:"value"`
		Proof []string     `json:"proof"`
	}

	type accountResult struct {
		Address      common.Address  `json:"address"`
		AccountProof []string        `json:"accountProof"`
		Balance      *hexutil.Big    `json:"balance"`
		CodeHash     common.Hash     `json:"codeHash"`
		Nonce        hexutil.Uint64  `json:"nonce"`
		StorageHash  common.Hash     `json:"storageHash"`
		StorageProof []storageResult `json:"storageProof"`
	}

	// Avoid keys being 'null'.
	if keys == nil {
		keys = []string{}
	}

	var res accountResult
	err := ec.c.CallContext(ctx, &res, "eth_getProof", account, keys, ethclient.ToBlockNumArg(blockNumber))
	// Turn hexutils back to normal datatypes
	storageResults := make([]StorageResult, 0, len(res.StorageProof))
	for _, st := range res.StorageProof {
		storageResults = append(storageResults, StorageResult{
			Key:   st.Key,
			Value: st.Value.ToInt(),
			Proof: st.Proof,
		})
	}
	result := AccountResult{
		Address:      res.Address,
		AccountProof: res.AccountProof,
		Balance:      res.Balance.ToInt(),
		Nonce:        uint64(res.Nonce),
		CodeHash:     res.CodeHash,
		StorageHash:  res.StorageHash,
		StorageProof: storageResults,
	}
	return &result, err
}

// OverrideAccount specifies the state of an account to be overridden.
type OverrideAccount struct {
	Nonce     uint64                      `json:"nonce"`
	Code      []byte                      `json:"code"`
	Balance   *big.Int                    `json:"balance"`
	State     map[common.Hash]common.Hash `json:"state"`
	StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
}

// CallContract executes a message call transaction, which is directly executed in the VM
// of the node, but never mined into the blockchain.
//
// blockNumber selects the block height at which the call runs. It can be nil, in which
// case the code is taken from the latest known block. Note that state from very old
// blocks might not be available.
//
// overrides specifies a map of contract states that should be overwritten before executing
// the message call.
// Please use ethclient.CallContract instead if you don't need the override functionality.
func (ec *Client) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int, overrides *map[common.Address]OverrideAccount) ([]byte, error) {
	var hex hexutil.Bytes
	err := ec.c.CallContext(
		ctx, &hex, "eth_call", toCallArg(msg),
		ethclient.ToBlockNumArg(blockNumber), toOverrideMap(overrides),
	)
	return hex, err
}

// GCStats retrieves the current garbage collection stats from a geth node.
func (ec *Client) GCStats(ctx context.Context) (*debug.GCStats, error) {
	var result debug.GCStats
	err := ec.c.CallContext(ctx, &result, "debug_gcStats")
	return &result, err
}

// MemStats retrieves the current memory stats from a geth node.
func (ec *Client) MemStats(ctx context.Context) (*runtime.MemStats, error) {
	var result runtime.MemStats
	err := ec.c.CallContext(ctx, &result, "debug_memStats")
	return &result, err
}

// SubscribePendingTransactions subscribes to new pending transactions.
func (ec *Client) SubscribePendingTransactions(ctx context.Context, ch chan<- common.Hash) (*rpc.ClientSubscription, error) {
	return ec.c.EthSubscribe(ctx, ch, "newPendingTransactions")
}

func toCallArg(msg ethereum.CallMsg) interface{} {
	arg := map[string]interface{}{
		"from": msg.From,
		"to":   msg.To,
	}
	if len(msg.Data) > 0 {
		arg["input"] = hexutil.Bytes(msg.Data)
	}
	if msg.Value != nil {
		arg["value"] = (*hexutil.Big)(msg.Value)
	}
	if msg.Gas != 0 {
		arg["gas"] = hexutil.Uint64(msg.Gas)
	}
	if msg.GasPrice != nil {
		arg["gasPrice"] = (*hexutil.Big)(msg.GasPrice)
	}
	if msg.GasFeeCap != nil {
		arg["maxFeePerGas"] = (*hexutil.Big)(msg.GasFeeCap)
	}
	if msg.GasTipCap != nil {
		arg["maxPriorityFeePerGas"] = (*hexutil.Big)(msg.GasTipCap)
	}
	if msg.AccessList != nil {
		arg["accessList"] = msg.AccessList
	}
	if msg.BlobGasFeeCap != nil {
		arg["maxFeePerBlobGas"] = (*hexutil.Big)(msg.BlobGasFeeCap)
	}
	if msg.BlobHashes != nil {
		arg["blobVersionedHashes"] = msg.BlobHashes
	}
	return arg
}

func toOverrideMap(overrides *map[common.Address]OverrideAccount) interface{} {
	if overrides == nil {
		return nil
	}
	type overrideAccount struct {
		Nonce     hexutil.Uint64              `json:"nonce"`
		Code      hexutil.Bytes               `json:"code"`
		Balance   *hexutil.Big                `json:"balance"`
		State     map[common.Hash]common.Hash `json:"state"`
		StateDiff map[common.Hash]common.Hash `json:"stateDiff"`
	}
	result := make(map[common.Address]overrideAccount)
	for addr, override := range *overrides {
		result[addr] = overrideAccount{
			Nonce:     hexutil.Uint64(override.Nonce),
			Code:      override.Code,
			Balance:   (*hexutil.Big)(override.Balance),
			State:     override.State,
			StateDiff: override.StateDiff,
		}
	}
	return &result
}
