// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

var noopRelease tracers.StateReleaseFunc = func() {}

// noEndOfBlockOps wraps [hook.Points] to suppress
// [hook.Points.EndOfBlockOps] and [hook.Points.AfterExecutingBlock], used by
// the tracer to skip end-of-block operations during partial replay.
//
// TODO(StephenButtolph): Properly abstract execution to not rely on method
// suppression. It is fragile and could result in accidentially modifying the
// block state or even disk state during tracing.
type noEndOfBlockOps struct {
	hook.Points
}

// EndOfBlockOps always returns nil.
func (noEndOfBlockOps) EndOfBlockOps(*types.Block) ([]hook.Op, error) { return nil, nil }

// AfterExecutingBlock always returns nil.
func (noEndOfBlockOps) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

func (b *backend) RPCEVMTimeout() time.Duration {
	return b.config.EVMTimeout
}

func (b *backend) RPCGasCap() uint64 {
	return b.config.GasCap
}

func (*backend) Engine() consensus.Engine {
	return (*coinbaseAsAuthor)(nil)
}

type coinbaseAsAuthor struct {
	consensus.Engine
}

func (*coinbaseAsAuthor) Author(h *types.Header) (common.Address, error) {
	return h.Coinbase, nil
}

func (b *backend) GetEVM(ctx context.Context, msg *core.Message, sdb *state.StateDB, hdr *types.Header, cfg *vm.Config, bCtx *vm.BlockContext) *vm.EVM {
	if bCtx == nil {
		bCtx = new(vm.BlockContext)
		*bCtx = core.NewEVMBlockContext(hdr, b.ChainContext(), &hdr.Coinbase)
	}
	txCtx := core.NewEVMTxContext(msg)
	return vm.NewEVM(*bCtx, txCtx, sdb, b.ChainConfig(), *cfg)
}

// StateAndHeaderByNumber performs the same faking as
// [backend.StateAndHeaderByNumberOrHash].
func (b *backend) StateAndHeaderByNumber(ctx context.Context, num rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	return b.StateAndHeaderByNumberOrHash(ctx, rpc.BlockNumberOrHashWithNumber(num))
}

// StateAndHeaderByNumberOrHash fakes the returned [types.Header] to contain
// post-execution results, mimicking a synchronous block. The [state.StateDB] is
// opened at the post-execution root, as carried by the faked header.
func (b *backend) StateAndHeaderByNumberOrHash(ctx context.Context, numOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if n, ok := numOrHash.Number(); ok && n == rpc.PendingBlockNumber {
		return nil, nil, errors.New("state not available for pending block")
	}

	// TODO(JonathanOppenheimer): [backend.restoreBlock] reads and decodes the
	// full block body and receipts but this method only needs the header, the
	// post-execution state root, and the executed base fee. Some sort of
	// refactor could improve performance.
	bl, err := b.restoreExecutedBlock(ctx, numOrHash)
	if err != nil {
		return nil, nil, err
	}

	hdr := executedHeader(bl)
	sdb, err := b.StateDB(hdr.Root)
	if err != nil {
		return nil, nil, err
	}
	return sdb, hdr, nil
}

// StateAtBlock returns the state database after executing the given block.
//
// The reexec, base, readOnly, and preferDisk parameters are ignored because SAE
// does not implement geth's re-execution-from-archive strategy.
//
// Like geth, SAE only stores historical state roots, not full historical state.
// The underlying trie data must still be present in the state cache/DB for
// [state.New] to succeed. This means tracing is limited to recent blocks whose
// trie data has not been pruned (or requires an archival node for older blocks).
//
// Reference: https://geth.ethereum.org/docs/developers/evm-tracing#state-availability
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (b *backend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	num := rpc.BlockNumber(block.NumberU64()) // #nosec G115 -- won't overflow for a while.
	bl, err := b.restoreExecutedBlock(ctx, rpc.BlockNumberOrHashWithNumber(num))
	if err != nil {
		return nil, nil, err
	}

	sdb, err := b.StateDB(bl.PostExecutionStateRoot())
	if err != nil {
		return nil, nil, err
	}
	return sdb, noopRelease, nil
}

// StateAtTransaction returns the execution environment of a particular
// transaction within a block. It replays all preceding transactions to produce
// the state just before the target transaction, then returns the message and
// block context needed for tracing.
//
// Replay calls [saexec.Execute] - the same pipeline used by
// [saexec.Executor] - with [noEndOfBlockOps] to suppress end-of-block
// operations and [saexec.NullReceiptStore] to skip receipt broadcasting.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (b *backend) StateAtTransaction(ctx context.Context, ethB *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	var bCtx vm.BlockContext
	if ethB.NumberU64() == 0 {
		return nil, bCtx, nil, nil, errors.New("no transactions in genesis")
	}
	txs := ethB.Transactions()
	if txIndex < 0 || txIndex >= len(txs) {
		return nil, bCtx, nil, nil, fmt.Errorf("transaction index %d out of range [0, %d)", txIndex, len(txs))
	}

	parent, err := b.restoreExecutedBlock(ctx, rpc.BlockNumberOrHashWithHash(ethB.ParentHash(), true /* canonical */))
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("restoring parent block: %w", err)
	}
	block, err := b.NewBlock(ethB, parent, nil)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing SAE block: %v", err)
	}

	// ethB was served by [tracerBackend], so its faked header carries the
	// executed base fee (see [executedHeader]), which the gas clock cannot
	// re-derive for pre-SAE blocks and the real header only bounds for
	// asynchronous ones. The faked base fee is never nil, so the guard only
	// avoids a panic in [uint256.FromBig].
	baseFee := new(uint256.Int)
	if bf := ethB.BaseFee(); bf != nil {
		var overflow bool
		baseFee, overflow = uint256.FromBig(bf)
		if overflow {
			return nil, bCtx, nil, nil, fmt.Errorf("base fee %v of block %d overflows 256 bits", bf, ethB.NumberU64())
		}
	}

	// Replay transactions 0..txIndex-1 to produce the state just before the
	// target transaction.
	result, err := saexec.Execute(
		block,
		b,
		txIndex,
		baseFee,
		noEndOfBlockOps{b.Hooks()},
		b.ChainConfig(),
		b.ChainContext(),
		&saexec.NullReceiptStore{},
		b.Logger(),
	)
	if err != nil {
		return nil, bCtx, nil, nil, err
	}

	msg, err := core.TransactionToMessage(txs[txIndex], result.Signer, result.BaseFee.ToBig())
	if err != nil {
		return nil, bCtx, nil, nil, err
	}
	return msg, result.BlockCtx, result.StateDB, noopRelease, nil
}

// tracerAPI serves the debug tracer APIs.
//
// Most APIs trace a full block from the parent's state. For these APIs, the
// backend returns the parent's state with the child block's before-block
// changes applied.
//
// debug_traceCall is special, because it doesn't expect the parent's state.
// Therefore, the state is returned without any before-block changes being
// applied by using a special backend.
type tracerAPI struct {
	*tracers.API
	traceCall *tracers.API
}

func newTracerAPI(b *backend) *tracerAPI {
	tb := &tracerBackend{b}
	return &tracerAPI{
		API:       tracers.NewAPI(tb),
		traceCall: tracers.NewAPI(&traceCallBackend{tb}),
	}
}

// TraceCall shadows [tracers.API.TraceCall] to serve it from
// [traceCallBackend] instead.
func (a *tracerAPI) TraceCall(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (any, error) {
	return a.traceCall.TraceCall(ctx, args, blockNrOrHash, config)
}

// traceCallBackend is [tracerBackend] except that StateAtBlock excludes the
// child block's before-block changes: debug_traceCall requests the state as
// of the block itself, not a base state for re-executing its child.
type traceCallBackend struct {
	*tracerBackend
}

func (b *traceCallBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.backend.StateAtBlock(ctx, block, reexec, base, readOnly, preferDisk)
}

var _ tracers.BlockHashOverrider = (*tracerBackend)(nil)

// tracerBackend adapts [backend] for the tracers API, faking headers to carry
// post-execution results and reporting canonical hashes for the faked blocks.
type tracerBackend struct {
	*backend
}

// StateAtBlock returns the state served by [backend.StateAtBlock] with the
// canonical child block's before-block state changes already applied, because
// the block-tracing endpoints request the state that the child's transactions
// ran on.
func (b *tracerBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	sdb, release, err := b.backend.StateAtBlock(ctx, block, reexec, base, readOnly, preferDisk)
	if err != nil {
		return nil, nil, err
	}

	if err := b.applyChildBeforeBlock(sdb, block.Header()); err != nil {
		release()
		return nil, nil, err
	}
	return sdb, release, nil
}

// applyChildBeforeBlock applies the canonical child block's pre-transaction
// state changes to sdb, the parent's post-execution state. It is a no-op if
// the parent has no canonical child.
func (b *tracerBackend) applyChildBeforeBlock(sdb *state.StateDB, parent *types.Header) error {
	child := rpc.BlockNumber(parent.Number.Uint64() + 1) // #nosec G115 -- won't overflow for a while.
	bl, err := b.restoreBlock(rpc.BlockNumberOrHashWithNumber(child))
	if errors.Is(err, blocks.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("restoring child block %d: %w", child, err)
	}
	ethB := bl.EthBlock()
	rules := b.ChainConfig().Rules(ethB.Number(), true /*isMerge*/, ethB.Time())
	// TODO(JonathanOppenheimer): once libevm's tracer APIs apply the EIP-4788
	// beacon root (already fixed upstream in geth), it will be applied twice,
	// so we should drop it here. [TestLibevmTracersDoNotApplyBeaconRoot] pins
	// the current libevm behaviour and will fail when that happens.
	return saexec.BeforeExecutingBlock(b.Hooks(), rules, sdb, parent, ethB)
}

// BlockHash returns the block's canonical hash, which differs from
// block.Hash() because the blocks served by this backend carry faked headers.
func (b *tracerBackend) BlockHash(block *types.Block) common.Hash {
	num := rpc.BlockNumber(block.NumberU64()) // #nosec G115 -- won't overflow for a while.
	bl, err := b.restoreBlock(rpc.BlockNumberOrHashWithNumber(num))
	if err != nil {
		b.Logger().Error("Restoring already-served block for its canonical hash",
			zap.Uint64("block_height", block.NumberU64()),
			zap.Error(err),
		)
		return block.Hash()
	}
	return bl.Hash()
}

// BlockByHash is the same as [backend.BlockByHash] but returns a faked header
// with post-execution results.
func (b *tracerBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.getBlockModified(ctx, rpc.BlockNumberOrHashWithHash(hash, true /* canonical */))
}

// BlockByNumber is the same as [backend.BlockByNumber] but returns a faked
// header with post-execution results.
func (b *tracerBackend) BlockByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Block, error) {
	return b.getBlockModified(ctx, rpc.BlockNumberOrHashWithNumber(n))
}

func (b *tracerBackend) getBlockModified(ctx context.Context, nOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	bl, err := b.restoreExecutedBlock(ctx, nOrHash)
	if err != nil {
		return nil, err
	}
	return bl.EthBlock().WithSeal(executedHeader(bl)), nil
}

// executedHeader returns the block's header faked to carry post-execution
// results (state root and base fee), mimicking a synchronous block.
//
// The API implementations expect this to be synchronous, sourcing the state
// root and the base fee from fields. The faked header's hash is wrong; the
// tracers API reports canonical hashes via [tracerBackend.BlockHash].
func executedHeader(bl *blocks.Block) *types.Header {
	hdr := bl.Header()
	hdr.Root = bl.PostExecutionStateRoot()
	hdr.BaseFee = bl.ExecutedBaseFee().ToBig()
	return hdr
}
