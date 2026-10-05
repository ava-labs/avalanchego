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
	recordGas := func(r *core.ExecutionResult) error {
		addGas(ctx, r.UsedGas)
		return nil
	}
	return b.BlockChainAPI.Call(ctx, args, blockNrOrHash, overrides, blockOverrides, ethapi.WithCallResultInterceptor(recordGas))
}
