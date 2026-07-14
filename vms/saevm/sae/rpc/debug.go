// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/eth/tracers/logger"
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
//   - [saexec.Execute] credits each transaction's burnt base fee via
//     hook.Points.AfterExecutingTransaction; libevm's loops do not.
//   - hook.Points.BeforeExecutingBlock writes upgrade-boundary state before a
//     block's first transaction while libevm's loops do not.
//   - A synchronous block replays with the base fee it was executed with,
//     which the gas clock cannot reproduce.
//
// The overrides replay via [backend.replay], which drives a
// [saexec.Execution] with full consensus-execution semantics.
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
	exec, err := t.b.replay(target.EthBlock(), target.ExecutedBaseFee())
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

// traceBlock traces each transaction in a single replay of the block: the
// tracer observes the same execution that advances the replay's state, so
// each transaction executes exactly once.
func (t *tracersAPI) traceBlock(ctx context.Context, numOrHash rpc.BlockNumberOrHash, config *tracers.TraceConfig) ([]*txTraceResult, error) {
	block, err := t.b.restoreBlock(numOrHash)
	if err != nil {
		return nil, err
	}
	if block.NumberU64() == 0 {
		return nil, errGenesisNotTraceable
	}

	timeout := tracers.DefaultTraceTimeout
	if config != nil && config.Timeout != nil {
		if timeout, err = time.ParseDuration(*config.Timeout); err != nil {
			return nil, err
		}
	}

	exec, err := t.b.replay(block.EthBlock(), block.ExecutedBaseFee())
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
		res, err := traceNextTx(ctx, exec, config, timeout, &tracers.Context{
			BlockHash:   blockHash,
			BlockNumber: blockNumber,
			TxIndex:     i,
			TxHash:      tx.Hash(),
		})
		if err != nil {
			return nil, err
		}
		results[i] = &txTraceResult{TxHash: tx.Hash(), Result: res}
	}
	return results, nil
}

// [traceNextTx] and [evmCancellingTracer] reimplement the tracer construction
// and per-transaction timeout guardrails of geth's (unexported)
// tracers.API.traceTx, as vendored in libevm:
// https://github.com/ava-labs/libevm/blob/b01f9ada7d62/eth/tracers/api.go#L937
//
// The code deliberately stays close to traceTx, with two deviations:
//
//   - traceTx executes the transaction itself with a bare core.ApplyMessage,
//     skipping the consensus-execution semantics described on [tracersAPI] —
//     which is why it cannot be reused. Here the transaction instead executes
//     inside [saexec.Execution.ExecuteNextTransaction], with the tracer
//     attached via [vm.Config].
//   - traceTx constructs the [vm.EVM] itself, giving it a handle on which to
//     call vm.EVM.Cancel on timeout. Here the EVM is constructed inside
//     core.ApplyTransaction, so [evmCancellingTracer] recovers the handle
//     from [vm.EVMLogger.CaptureStart] instead.
//
// traceNextTx executes exec's next transaction with the configured tracer
// attached, under the timeout guardrails described above.
func traceNextTx(ctx context.Context, exec *saexec.Execution, config *tracers.TraceConfig, timeout time.Duration, txctx *tracers.Context) (any, error) {
	if config == nil {
		config = &tracers.TraceConfig{}
	}
	var inner tracers.Tracer
	if config.Tracer != nil {
		var err error
		if inner, err = tracers.DefaultDirectory.New(*config.Tracer, txctx, config.TracerConfig); err != nil {
			return nil, err
		}
	} else {
		// Default tracer is the struct logger
		inner = logger.NewStructLogger(config.Config)
	}
	tracer := &evmCancellingTracer{Tracer: inner}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	go func() {
		<-deadlineCtx.Done()
		if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
			tracer.Stop(errors.New("execution timeout"))
		}
	}()

	if _, err := exec.ExecuteNextTransaction(vm.Config{Tracer: tracer}); err != nil {
		return nil, err
	}
	return tracer.GetResult()
}

// evmCancellingTracer wraps a [tracers.Tracer] so that [tracers.Tracer.Stop]
// also cancels the [vm.EVM] executing the traced transaction, mirroring the
// vmenv.Cancel() performed by geth's traceTx (see [traceNextTx]).
type evmCancellingTracer struct {
	tracers.Tracer

	mu      sync.Mutex
	env     *vm.EVM
	stopped bool
}

func (t *evmCancellingTracer) CaptureStart(env *vm.EVM, from, to common.Address, create bool, input []byte, gas uint64, value *big.Int) {
	t.mu.Lock()
	t.env = env
	stopped := t.stopped
	t.mu.Unlock()

	if stopped {
		env.Cancel()
	}
	t.Tracer.CaptureStart(env, from, to, create, input, gas, value)
}

func (t *evmCancellingTracer) Stop(err error) {
	t.mu.Lock()
	t.stopped = true
	env := t.env
	t.mu.Unlock()

	t.Tracer.Stop(err)
	if env != nil {
		env.Cancel()
	}
}
