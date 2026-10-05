// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"
)

// Call overrides [ethapi.BlockChainAPI.Call] to record the gas used for
// [GasUsedHeader].
func (b *blockChainAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (hexutil.Bytes, error) {
	return b.BlockChainAPI.Call(ctx, args, blockNrOrHash, overrides, blockOverrides, recordGas(ctx))
}

// recordGas returns an option that adds the gas used by a call to the
// [GasUsedHeader] of the request carried by ctx.
func recordGas(ctx context.Context) ethapi.CallOption {
	return ethapi.WithCallResultInterceptor(func(r *core.ExecutionResult) error {
		addGas(ctx, r.UsedGas)
		return nil
	})
}
