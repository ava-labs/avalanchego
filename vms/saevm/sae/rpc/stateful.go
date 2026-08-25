// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/consensus"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/rpc"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
)

var noopRelease tracers.StateReleaseFunc = func() {}

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
	if !b.config.ResolvePendingToLastExecuted {
		if n, ok := numOrHash.Number(); ok && n == rpc.PendingBlockNumber {
			return nil, nil, errors.New("state not available for pending block")
		}
	}

	// TODO(JonathanOppenheimer): [backend.restoreExecutedBlock] reads and
	// decodes the full block body and receipts but this method only needs the
	// header, the post-execution state root, and the executed base fee. Some
	// sort of refactor could improve performance.
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
	sdb, _, err := b.stateAtBlock(ctx, block.NumberU64())
	if err != nil {
		return nil, nil, err
	}
	return sdb, noopRelease, nil
}

// stateAtBlock returns the state after executing block num, along with the
// stored block it was restored from.
func (b *backend) stateAtBlock(ctx context.Context, num uint64) (*state.StateDB, *blocks.Block, error) {
	n := rpc.BlockNumber(num) // #nosec G115 -- won't overflow for a while.
	bl, err := b.restoreExecutedBlock(ctx, rpc.BlockNumberOrHashWithNumber(n))
	if err != nil {
		return nil, nil, err
	}

	sdb, err := b.StateDB(bl.PostExecutionStateRoot())
	if err != nil {
		return nil, nil, err
	}
	return sdb, bl, nil
}

// StateAtTransaction returns the execution environment of a particular
// transaction within a block. It replays all preceding transactions to produce
// the state just before the target transaction, then returns the message and
// block context needed for tracing. Replay does not apply end-of-block
// operations, record block progress, or publish receipts.
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

	parent, err := b.restoreExecutedParent(ctx, ethB)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("restoring parent block: %w", err)
	}
	block, err := b.NewBlock(ethB, parent, nil)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing SAE block: %v", err)
	}
	stateDB, err := b.StateDB(parent.PostExecutionStateRoot())
	if err != nil {
		return nil, bCtx, nil, nil, err
	}

	// Replay transactions 0..txIndex-1 to produce the state just before the
	// target transaction.
	result, err := saexec.Execute(
		block,
		stateDB,
		b.Hooks(),
		b.ChainConfig(),
		b.ChainContext(),
		b.Logger(),
		saexec.WithMaxNumTxs(uint(txIndex)),
		saexec.SkipEndOfBlockOps(),
	)
	if err != nil {
		return nil, bCtx, nil, nil, err
	}

	msg, err := core.TransactionToMessage(txs[txIndex], result.Signer, result.BaseFee.ToBig())
	if err != nil {
		return nil, bCtx, nil, nil, err
	}
	return msg, result.BlockCtx, stateDB, noopRelease, nil
}

// tracerAPI serves the debug tracer APIs, routing each endpoint to a
// [tracers.API] over whichever backend supplies the state that endpoint
// expects. See this package's README for the full mapping and each backend's
// purpose.
type tracerAPI struct {
	*tracers.API
	tracerBackend *tracerBackend
	traceCall     *tracers.API
}

func newTracerAPI(b *backend) *tracerAPI {
	tb := &tracerBackend{b}
	return &tracerAPI{
		API:           tracers.NewAPI(tb),
		tracerBackend: tb,
		traceCall:     tracers.NewAPI(&traceCallBackend{tb}),
	}
}

// TraceCall shadows [tracers.API.TraceCall] to serve it from
// [traceCallBackend] instead.
func (a *tracerAPI) TraceCall(ctx context.Context, args ethapi.TransactionArgs, blockNrOrHash rpc.BlockNumberOrHash, config *tracers.TraceCallConfig) (any, error) {
	return a.traceCall.TraceCall(ctx, args, blockNrOrHash, config)
}

// TraceBlock shadows [tracers.API.TraceBlock] to replace the caller-supplied
// block's worst-case base fee with the executed base fee before delegating.
// The block need not be canonical, but its parent MUST be.
//
// A synchronous (pre-SAE) block is traced with the base fee as supplied, which
// is the real fee paid by its transactions.
func (a *tracerAPI) TraceBlock(ctx context.Context, blob hexutil.Bytes, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, error) {
	block := new(types.Block)
	if err := rlp.DecodeBytes(blob, block); err != nil {
		return nil, fmt.Errorf("decoding block: %v", err)
	}
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable") // Copied from [tracers.TraceBlock]
	}

	// A synchronous (pre-SAE) block carries the base fee its transactions
	// actually paid, so it is traced as supplied.
	resealed := block
	if hdr := block.Header(); !hook.Synchronous(a.tracerBackend.Hooks(), hdr) {
		parent, err := a.tracerBackend.restoreExecutedParent(ctx, block)
		if err != nil {
			return nil, fmt.Errorf("restoring parent block: %w", err)
		}
		// The parent's gas clock, advanced to the start of the block,
		// determines the executed base fee, so the supplied one is discarded.
		gasClock := parent.ExecutedByGasTime()
		gasClock.BeforeBlock(a.tracerBackend.Hooks().BlockTime(hdr))
		hdr.BaseFee = gasClock.BaseFee().ToBig()
		resealed = block.WithSeal(hdr)
	}

	api := tracers.NewAPI(&suppliedHashBackend{
		tracerBackend: a.tracerBackend,
		supplied:      block,
		resealed:      resealed,
	})
	return tracers.TraceBlock(ctx, api, resealed, config)
}

// TraceBlockFromFile shadows [tracers.API.TraceBlockFromFile], which would
// otherwise call [tracers.API.TraceBlock] rather than [tracerAPI.TraceBlock].
func (a *tracerAPI) TraceBlockFromFile(ctx context.Context, file string, config *tracers.TraceConfig) ([]*tracers.TxTraceResult, error) {
	blob, err := os.ReadFile(file) //#nosec G304 -- Reading a caller-supplied file is the whole point.
	if err != nil {
		return nil, fmt.Errorf("reading file: %v", err)
	}
	return a.TraceBlock(ctx, blob, config)
}

var _ tracers.BlockHashOverrider = (*tracerBackend)(nil)

// tracerBackend adapts [backend] for the tracers API, faking headers to carry
// post-execution results and reporting canonical hashes for the faked blocks.
type tracerBackend struct {
	*backend
}

// StateAtBlock returns the state served by [backend.StateAtBlock] with the
// canonical child block's start-executing-block state changes already applied,
// because the block-tracing endpoints request the state that the child's
// transactions ran on.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (b *tracerBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	num := rpc.BlockNumber(block.NumberU64() + 1) // #nosec G115 -- won't overflow for a while.
	child, err := b.backend.BlockByNumber(ctx, num)
	if err != nil {
		return nil, nil, fmt.Errorf("reading child block %d: %w", num, err)
	}
	if child == nil {
		// This backend only traces canonical blocks ([suppliedHashBackend]
		// serves the rest), so the child MUST exist.
		return nil, nil, fmt.Errorf("no canonical child of block %d", block.NumberU64())
	}
	return b.stateAtBlockWithChild(ctx, block.NumberU64(), child)
}

// stateAtBlockWithChild returns the parent's post-execution state with the
// child block's pre-transaction state changes applied.
func (b *tracerBackend) stateAtBlockWithChild(ctx context.Context, n uint64, child *types.Block) (*state.StateDB, tracers.StateReleaseFunc, error) {
	sdb, parentBlock, err := b.backend.stateAtBlock(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	block, err := b.NewBlock(child, parentBlock, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("constructing SAE block: %v", err)
	}

	// TODO(JonathanOppenheimer): once libevm's tracer APIs apply the EIP-4788
	// beacon root (already fixed upstream in geth), it will be applied twice,
	// so we should drop it here.
	_, err = saexec.Execute(
		block,
		sdb,
		b.Hooks(),
		b.ChainConfig(),
		b.ChainContext(),
		b.Logger(),
		saexec.WithMaxNumTxs(0),
		saexec.SkipEndOfBlockOps(),
	)
	if err != nil {
		return nil, nil, err
	}
	return sdb, noopRelease, nil
}

// StateAtTransaction returns the state served by [backend.StateAtTransaction]
// with the provided block overridden by the stored one, because the faked
// header this backend serves MUST NOT reach the replay's hooks.
func (b *tracerBackend) StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	var bCtx vm.BlockContext
	num := rpc.BlockNumber(block.NumberU64()) // #nosec G115 -- won't overflow for a while.
	stored, err := b.backend.BlockByNumber(ctx, num)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("reading block %d: %w", num, err)
	}
	if stored == nil {
		// This backend only traces canonical blocks, so it MUST exist.
		return nil, bCtx, nil, nil, fmt.Errorf("no canonical block %d", num)
	}
	return b.backend.StateAtTransaction(ctx, stored, txIndex, reexec)
}

// BlockHash returns the block's canonical hash, which differs from
// block.Hash() because the blocks served by this backend carry faked headers.
func (b *tracerBackend) BlockHash(block *types.Block) common.Hash {
	num := block.NumberU64()
	hash := rawdb.ReadCanonicalHash(b.DB(), num)
	if hash == (common.Hash{}) {
		b.Logger().Error("missing canonical hash override for block",
			zap.Uint64("block_height", num),
		)
		return block.Hash()
	}
	return hash
}

// BlockByHash is the same as [backend.BlockByHash] but with a faked header
// carrying post-execution results.
func (b *tracerBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.blockWithExecutedHeader(ctx, rpc.BlockNumberOrHashWithHash(hash, true /* canonical */))
}

// BlockByNumber is the same as [backend.BlockByNumber] but with a faked header
// carrying post-execution results.
func (b *tracerBackend) BlockByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Block, error) {
	return b.blockWithExecutedHeader(ctx, rpc.BlockNumberOrHashWithNumber(n))
}

func (b *tracerBackend) blockWithExecutedHeader(ctx context.Context, nOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	bl, err := b.restoreExecutedBlock(ctx, nOrHash)
	if err != nil {
		return nil, err
	}
	return bl.EthBlock().WithSeal(executedHeader(bl)), nil
}

// executedHeader returns the block's header faked to carry post-execution
// results, mimicking a synchronous block. The API implementations can then
// source the state root and base fee from header fields. The faked header
// hashes to the wrong value, which [tracerBackend.BlockHash] corrects.
func executedHeader(bl *blocks.Block) *types.Header {
	hdr := bl.Header()
	hdr.Root = bl.PostExecutionStateRoot()
	hdr.BaseFee = bl.ExecutedBaseFee().ToBig()
	return hdr
}

// suppliedHashBackend is a per-trace [tracerBackend] for the caller-supplied
// block re-sealed by [tracerAPI.TraceBlock]. The block MAY be non-canonical.
type suppliedHashBackend struct {
	*tracerBackend
	supplied *types.Block
	resealed *types.Block
}

// BlockHash returns the hash the caller supplied for the re-sealed block and
// the canonical hash for every other block.
func (b *suppliedHashBackend) BlockHash(block *types.Block) common.Hash {
	if block.Hash() == b.resealed.Hash() {
		return b.supplied.Hash()
	}
	return b.tracerBackend.BlockHash(block)
}

// StateAtBlock returns the parent's post-execution state with the supplied
// block's start-executing-block changes applied. The hooks see the block as
// supplied, not as re-sealed, so tracing a canonical block by RLP matches
// tracing it by number.
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (b *suppliedHashBackend) StateAtBlock(ctx context.Context, parent *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.stateAtBlockWithChild(ctx, parent.NumberU64(), b.supplied)
}

// traceCallBackend is [tracerBackend] except that StateAtBlock excludes the
// child block's start-executing-block changes. debug_traceCall is expected to
// behave as if it is executing immediately after the requested block.
type traceCallBackend struct {
	*tracerBackend
}

func (b *traceCallBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	return b.tracerBackend.backend.StateAtBlock(ctx, block, reexec, base, readOnly, preferDisk)
}
