// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

func (b *backend) SetHead(uint64) {
	b.Logger().Info("debug_setHead called but not supported by SAE")
}

// tracersAPI wraps libevm's [tracers.API], overriding endpoints that must
// replay via [saexec.Execute] to apply [hook.Points], which libevm's
// core.ApplyMessage misses.
type tracersAPI struct {
	*tracers.API
	b *backend
}

var errGenesisNotTraceable = errors.New("genesis is not traceable")

// IntermediateRoots returns the state root after each of the block's
// transactions. As in coreth, the roots exclude end-of-block operations.
func (t *tracersAPI) IntermediateRoots(ctx context.Context, hash common.Hash, _ *tracers.TraceConfig) ([]common.Hash, error) {
	target, err := t.b.restoreBlock(rpc.BlockNumberOrHashWithHash(hash, false))
	if err != nil {
		return nil, err
	}
	if target.NumberU64() == 0 {
		return nil, errGenesisNotTraceable
	}
	result, err := t.b.replayBlock(target.EthBlock(), math.MaxInt, saexec.WithIntermediateRoots())
	if err != nil {
		return nil, err
	}
	return result.IntermediateRoots, nil
}

// txTraceResult matches the JSON shape of the embedded block-tracing
// endpoints' unexported result type
type txTraceResult struct {
	TxHash common.Hash `json:"txHash"`
	Result any         `json:"result,omitempty"`
}

// TraceBlockByNumber traces each of the block's transactions; see
// [tracersAPI.traceBlock].
func (t *tracersAPI) TraceBlockByNumber(ctx context.Context, number rpc.BlockNumber, config *tracers.TraceConfig) ([]*txTraceResult, error) {
	return t.traceBlock(ctx, rpc.BlockNumberOrHashWithNumber(number), config)
}

// TraceBlockByHash traces each of the block's transactions; see
// [tracersAPI.traceBlock].
func (t *tracersAPI) TraceBlockByHash(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) ([]*txTraceResult, error) {
	return t.traceBlock(ctx, rpc.BlockNumberOrHashWithHash(hash, false), config)
}

// traceBlock traces each transaction against its pre-state from
// [backend.StateAtTransaction], via [tracers.TraceTx] to keep libevm's
// per-transaction timeout and cancellation guardrails.
func (t *tracersAPI) traceBlock(ctx context.Context, numOrHash rpc.BlockNumberOrHash, config *tracers.TraceConfig) ([]*txTraceResult, error) {
	block, err := t.b.restoreBlock(numOrHash)
	if err != nil {
		return nil, err
	}
	if block.NumberU64() == 0 {
		return nil, errGenesisNotTraceable
	}

	txs := block.Transactions()
	results := make([]*txTraceResult, len(txs))
	for i, tx := range txs {
		msg, vmctx, statedb, release, err := t.b.StateAtTransaction(ctx, block.EthBlock(), i, 0)
		if err != nil {
			return nil, err
		}
		res, err := tracers.TraceTx(t.API, ctx, msg, &tracers.Context{
			BlockHash:   block.Hash(),
			BlockNumber: block.Number(),
			TxIndex:     i,
			TxHash:      tx.Hash(),
		}, vmctx, statedb, config)
		release()
		if err != nil {
			return nil, err
		}
		results[i] = &txTraceResult{TxHash: tx.Hash(), Result: res}
	}
	return results, nil
}
