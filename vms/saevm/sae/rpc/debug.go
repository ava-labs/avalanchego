// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

func (b *backend) SetHead(uint64) {
	b.Logger().Info("debug_setHead called but not supported by SAE")
}

// tracersAPI selectively serves libevm's [tracers.API], reimplementing the
// endpoints that re-execute a block's transactions in an internal loop of bare
// core.ApplyMessage calls, which diverges from [saexec.Execute]:
//
//   - [saexec.Execute] credits each transaction's burnt base fee via
//     hook.Points.AfterExecutingTransaction; libevm's loops do not.
//   - hook.Points.BeforeExecutingBlock writes upgrade-boundary state before a
//     block's first transaction while libevm's loops do not.
//   - A synchronous block replays with the base fee it was executed with,
//     which the gas clock cannot reproduce.
//
// The reimplementations replay via [backend.replay], which drives a
// [saexec.Execution] with full consensus-execution semantics.
//
// TODO(JonathanOppenheimer): properly support the remaining [tracers.API]
// endpoints.
type tracersAPI struct {
	inner *tracers.API
	b     *backend
}

func newTracersAPI(b *backend) *tracersAPI {
	return &tracersAPI{
		inner: tracers.NewAPI(b),
		b:     b,
	}
}

// restoreAndReplay restores the block identified by numOrHash and begins a
// replay of its transactions with the base fee it was originally executed with
// (see [backend.replay]).
func (t *tracersAPI) restoreAndReplay(ctx context.Context, numOrHash rpc.BlockNumberOrHash) (*blocks.Block, *saexec.Execution, error) {
	target, err := t.b.restoreBlock(numOrHash)
	if err != nil {
		return nil, nil, err
	}
	if err := target.WaitUntilExecuted(ctx); err != nil {
		return nil, nil, err
	}
	exec, err := t.b.replay(ctx, target.EthBlock(), target.ExecutedBaseFee())
	if err != nil {
		return nil, nil, err
	}
	return target, exec, nil
}

// IntermediateRoots returns the state root after each of the block's
// transactions. The roots exclude end-of-block operations.
func (t *tracersAPI) IntermediateRoots(ctx context.Context, hash common.Hash, _ *tracers.TraceConfig) ([]common.Hash, error) {
	target, exec, err := t.restoreAndReplay(ctx, rpc.BlockNumberOrHashWithHash(hash, true /* canonical */))
	if err != nil {
		return nil, err
	}

	var roots []common.Hash
	for range target.Transactions() {
		if _, err := exec.ExecuteNextTransaction(vm.Config{}); err != nil {
			return nil, err
		}
		roots = append(roots, exec.IntermediateRoot())
	}
	return roots, nil
}

// txTraceResult matches the JSON shape of the embedded block-tracing
// endpoints' unexported result type.
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

// TraceTransaction delegates to libevm's implementation, which replays the
// preceding transactions via [backend.StateAtTransaction] and so preserves
// consensus-execution semantics.
func (t *tracersAPI) TraceTransaction(ctx context.Context, hash common.Hash, config *tracers.TraceConfig) (any, error) {
	return t.inner.TraceTransaction(ctx, hash, config)
}

// TraceCall delegates to libevm's implementation, which sources its state via
// [backend.StateAtBlock] and so needs no replay loop of its own.
func (t *tracersAPI) TraceCall(ctx context.Context, args ethapi.TransactionArgs, numOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (any, error) {
	return t.inner.TraceCall(ctx, args, numOrHash, config)
}

// traceBlock traces each transaction in a single replay of the block: the
// tracer observes the same execution that advances the replay's state, so
// each transaction executes exactly once.
func (t *tracersAPI) traceBlock(ctx context.Context, numOrHash rpc.BlockNumberOrHash, config *tracers.TraceConfig) ([]*txTraceResult, error) {
	block, exec, err := t.restoreAndReplay(ctx, numOrHash)
	if err != nil {
		return nil, err
	}

	var (
		blockHash   = block.Hash()
		blockNumber = block.Number()
		txs         = block.Transactions()
		results     = make([]*txTraceResult, len(txs))
	)
	for i, tx := range txs {
		txctx := &tracers.Context{
			BlockHash:   blockHash,
			BlockNumber: blockNumber,
			TxIndex:     i,
			TxHash:      tx.Hash(),
		}
		res, err := tracers.TraceTx(ctx, config, txctx, func(cfg vm.Config) error {
			_, err := exec.ExecuteNextTransaction(cfg)
			return err
		})
		if err != nil {
			return nil, err
		}
		results[i] = &txTraceResult{TxHash: tx.Hash(), Result: res}
	}
	return results, nil
}
