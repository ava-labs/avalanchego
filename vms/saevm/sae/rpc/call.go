// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"
)

// Call overrides [ethapi.BlockChainAPI.Call] to record the gas used for
// [GasUsedHeader], which libevm discards.
//
// Copied from [ethapi.BlockChainAPI.Call], with the addition of [addGas].
// See https://github.com/ava-labs/libevm/blob/dbf7ede95a25d8dfbdadafe53822022cba222d71/internal/ethapi/api.go#L1147-L1161
func (b *blockChainAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (hexutil.Bytes, error) {
	if blockNrOrHash == nil {
		latest := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
		blockNrOrHash = &latest
	}
	result, err := ethapi.DoCall(ctx, b.b, args, *blockNrOrHash, overrides, blockOverrides, b.b.RPCEVMTimeout(), b.b.RPCGasCap())
	if err != nil {
		return nil, err
	}
	addGas(ctx, result.UsedGas)
	if len(result.Revert()) > 0 {
		return nil, ethapi.NewRevertError(result.Revert())
	}
	return result.Return(), result.Err
}
