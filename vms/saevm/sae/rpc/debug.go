// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

func (b *backend) SetHead(uint64) {
	b.Logger().Info("debug_setHead called but not supported by SAE")
}

// tracersAPI wraps libevm's [tracers.API], overriding the endpoints that
// re-execute a block's transactions in an internal loop of bare
// core.ApplyMessage calls, which diverges from [saexec.Execute]:
//
//   - [saexec.Execute] credits each transaction's burnt base fee to an
//     explicit burn address (see hook.Points.BaseFeeBurnAddress).
//   - hook.Points.BeforeExecutingBlock writes upgrade-boundary state before a
//     block's first transaction; libevm's loops do not.
//   - A synchronous block replays with the base fee it was executed with,
//     which the gas clock cannot reproduce.
//
// The overrides replay via [backend.replayBlock] and
// [backend.StateAtTransaction], both built on [saexec.Execute].
type tracersAPI struct {
	*tracers.API
	b *backend
}

func newTracersAPI(b *backend) *tracersAPI {
	return &tracersAPI{
		API: tracers.NewAPI(b),
		b:   b,
	}
}

var errGenesisNotTraceable = errors.New("genesis is not traceable")

// IntermediateRoots returns the state root after each of the block's
// transactions. The roots exclude end-of-block operations.
func (t *tracersAPI) IntermediateRoots(ctx context.Context, hash common.Hash, _ *tracers.TraceConfig) ([]common.Hash, error) {
	target, err := t.b.restoreBlock(rpc.BlockNumberOrHashWithHash(hash, true /* canonical */))
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
	return t.traceBlock(ctx, rpc.BlockNumberOrHashWithHash(hash, true /* canonical */), config)
}

// The remaining embedded [tracers.API] endpoints that replay blocks run the
// same divergent core.ApplyMessage loops described on [tracersAPI], so they
// are masked rather than served incorrectly.
//
// TODO(JonathanOppenheimer): properly support these RPCs.
var errNotSupported = errors.New("not supported by SAE")

func (*tracersAPI) TraceChain(context.Context, rpc.BlockNumber, rpc.BlockNumber, *tracers.TraceConfig) ([]*txTraceResult, error) {
	return nil, errNotSupported
}

func (*tracersAPI) TraceBlock(context.Context, hexutil.Bytes, *tracers.TraceConfig) ([]*txTraceResult, error) {
	return nil, errNotSupported
}

func (*tracersAPI) TraceBlockFromFile(context.Context, string, *tracers.TraceConfig) ([]*txTraceResult, error) {
	return nil, errNotSupported
}

func (*tracersAPI) StandardTraceBlockToFile(context.Context, common.Hash, *tracers.StdTraceConfig) ([]string, error) {
	return nil, errNotSupported
}

func (*tracersAPI) StandardTraceBadBlockToFile(context.Context, common.Hash, *tracers.StdTraceConfig) ([]string, error) {
	return nil, errNotSupported
}

// traceBlock traces each transaction against its pre-state from
// [backend.StateAtTransaction], via [tracers.TraceTx] to keep libevm's
// per-transaction timeout and cancellation guardrails.
//
// TODO(JonathanOppenheimer): each [backend.StateAtTransaction] call replays
// the transaction prefix, making a block trace O(|txs|^2) executions.
// This is too slow.
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
