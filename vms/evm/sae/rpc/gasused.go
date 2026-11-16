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

// GasUsedHeader is the HTTP response header carrying the total gas used by
// every eth_call in the request, batches included. It is a decimal integer and
// is omitted if no eth_call completed.
const GasUsedHeader = "Gas-Used"

// addGas records gas used by a call in the request carried by ctx. It is a
// no-op if ctx was not created by [withResponseHeaders].
func addGas(ctx context.Context, gas uint64) {
	rh, ok := ctx.Value(responseHeadersKey{}).(*responseHeaders)
	if !ok {
		return
	}
	rh.mu.Lock()
	defer rh.mu.Unlock()
	rh.gas += gas
	rh.hasGas = true
}

// Call overrides [ethapi.BlockChainAPI.Call] to record the gas used for
// [GasUsedHeader]. The interceptor needs the request's context, so it is set
// on a [ethapi.BlockChainAPI] built for each call.
func (b *blockChainAPI) Call(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash *rpc.BlockNumberOrHash, overrides *ethapi.StateOverride, blockOverrides *ethapi.BlockOverrides) (hexutil.Bytes, error) {
	recordGas := func(r *core.ExecutionResult) error {
		addGas(ctx, r.UsedGas)
		return nil
	}
	api := ethapi.NewBlockChainAPI(b.b, ethapi.WithDefaultCallOptions(ethapi.WithCallResultInterceptor(recordGas)))
	return api.Call(ctx, args, blockNrOrHash, overrides, blockOverrides)
}
