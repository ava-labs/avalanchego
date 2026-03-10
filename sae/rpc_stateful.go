// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/consensus"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saexec"
)

var noopRelease tracers.StateReleaseFunc = func() {}

// noEndOfBlockOps wraps [hook.Points] to suppress
// [hook.Points.EndOfBlockOps], used by the tracer to skip end-of-block
// operations during partial replay.
type noEndOfBlockOps struct {
	hook.Points
}

// EndOfBlockOps always returns nil.
func (noEndOfBlockOps) EndOfBlockOps(*types.Block) ([]hook.Op, error) { return nil, nil }

func (b *apiBackend) RPCEVMTimeout() time.Duration {
	return b.vm.config.RPCConfig.EVMTimeout
}

func (b *apiBackend) RPCGasCap() uint64 {
	return b.vm.config.RPCConfig.GasCap
}

func (b *apiBackend) Engine() consensus.Engine {
	return (*coinbaseAsAuthor)(nil)
}

type coinbaseAsAuthor struct {
	consensus.Engine
}

func (*coinbaseAsAuthor) Author(h *types.Header) (common.Address, error) {
	return h.Coinbase, nil
}

func (b *apiBackend) GetEVM(ctx context.Context, msg *core.Message, sdb *state.StateDB, hdr *types.Header, cfg *vm.Config, bCtx *vm.BlockContext) *vm.EVM {
	if bCtx == nil {
		bCtx = new(vm.BlockContext)
		*bCtx = core.NewEVMBlockContext(hdr, b.vm.exec.ChainContext(), &hdr.Coinbase)
	}
	txCtx := core.NewEVMTxContext(msg)
	return vm.NewEVM(*bCtx, txCtx, sdb, b.ChainConfig(), *cfg)
}

// StateAndHeaderByNumber performs the same faking as
// [apiBackend.StateAndHeaderByNumberOrHash].
func (b *apiBackend) StateAndHeaderByNumber(ctx context.Context, num rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	return b.StateAndHeaderByNumberOrHash(ctx, rpc.BlockNumberOrHashWithNumber(num))
}

// StateAndHeaderByNumberOrHash fakes the returned [types.Header] to contain
// post-execution results, mimicking a synchronous block. The [state.StateDB] is
// opened at the post-execution root, as carried by the faked header.
func (b *apiBackend) StateAndHeaderByNumberOrHash(ctx context.Context, numOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if n, ok := numOrHash.Number(); ok && n == rpc.PendingBlockNumber {
		return nil, nil, errors.New("state not available for pending block")
	}

	num, hash, err := b.resolveBlockNumberOrHash(numOrHash)
	if err != nil {
		return nil, nil, err
	}

	// The API implementations expect this to be synchronous, sourcing the state
	// root and the base fee from fields. At the time of writing, the returned
	// header's hash is never used so it's safe to modify it.
	//
	// TODO(arr4n) the above assumption is brittle under geth/libevm updates;
	// devise an approach to ensure that it is confirmed on each.
	var hdr *types.Header
	if bl, ok := b.vm.blocks.Load(hash); ok {
		hdr = bl.Header()
		hdr.Root = bl.PostExecutionStateRoot()
		hdr.BaseFee = bl.BaseFee().ToBig()
	} else {
		hdr = rawdb.ReadHeader(b.vm.db, hash, num)

		// TODO(arr4n) export [blocks.executionResults] to avoid multiple
		// database reads and canoto unmarshallings here.
		var err error
		hdr.Root, err = blocks.PostExecutionStateRoot(b.vm.xdb, num)
		if err != nil {
			return nil, nil, err
		}

		bf, err := blocks.ExecutionBaseFee(b.vm.xdb, num)
		if err != nil {
			return nil, nil, err
		}
		hdr.BaseFee = bf.ToBig()
	}

	sdb, err := state.New(hdr.Root, b.vm.exec.StateCache(), nil)
	if err != nil {
		return nil, nil, err
	}
	return sdb, hdr, nil
}

// StateAtBlock returns the state database after executing the given block. The
// reexec, base, readOnly, and preferDisk parameters are ignored because SAE
// does not implement geth's re-execution-from-archive strategy.
//
// Like geth, SAE only stores historical state roots, not full historical state.
// The underlying trie data must still be present in the state cache/DB for
// [state.New] to succeed. This means tracing is limited to recent blocks whose
// trie data has not been pruned (or requires an archival node for older blocks).
//
// Reference: https://geth.ethereum.org/docs/developers/evm-tracing#state-availability
func (b *apiBackend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	root, err := b.vm.postExecutionStateRoot(block.Hash(), block.NumberU64())
	if err != nil {
		return nil, nil, err
	}

	sdb, err := b.exec.StateDB(root)
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
func (b *apiBackend) StateAtTransaction(ctx context.Context, ethB *types.Block, txIndex int, reexec uint64) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	var bCtx vm.BlockContext
	if ethB.NumberU64() == 0 {
		return nil, bCtx, nil, nil, errors.New("no transactions in genesis")
	}
	txs := ethB.Transactions()
	if txIndex < 0 || txIndex >= len(txs) {
		return nil, bCtx, nil, nil, fmt.Errorf("transaction index %d out of range [0, %d)", txIndex, len(txs))
	}

	if b.vm.exec.LastExecuted().NumberU64() < ethB.NumberU64()-1 {
		return nil, bCtx, nil, nil, fmt.Errorf("parent of block %d not executed yet", ethB.NumberU64())
	}
	parent, err := b.vm.blockBuilder.new(
		// The I(E) check above guarantees D(A) of the same block; see
		// ../docs/invariants.md for details.
		rawdb.ReadBlock(b.db, ethB.ParentHash(), ethB.NumberU64()-1),
		// Ancestry is irrelevant for the parent as we just want its
		// post-execution artefacts.
		nil, nil,
	)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing parent block: %v", err)
	}
	if err := parent.RestoreExecutionArtefacts(b.vm.db, b.vm.xdb, b.ChainConfig()); err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("parent %T.RestoreExecutionArtefacts(...): %v", parent, err)
	}

	block, err := b.vm.blockBuilder.new(ethB, parent, nil)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing SAE block: %v", err)
	}

	// Replay transactions 0..txIndex-1 to produce the state just before the
	// target transaction.
	result, err := saexec.Execute(
		block,
		b.vm.exec,
		txIndex,
		noEndOfBlockOps{Points: b.vm.hooks},
		b.ChainConfig(),
		b.vm.exec.ChainContext(),
		&saexec.NullReceiptStore{},
		b.vm.log(),
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

// postExecutionStateRoot returns the post-execution state root for the block
// identified by hash and number, checking in-memory blocks first, then falling
// back to disk.
func (vm *VM) postExecutionStateRoot(hash common.Hash, num uint64) (common.Hash, error) {
	switch bl, ok := vm.blocks.Load(hash); {
	case !ok:
		return blocks.PostExecutionStateRoot(vm.xdb, num)
	case bl.Executed():
		return bl.PostExecutionStateRoot(), nil
	default:
		return common.Hash{}, fmt.Errorf("post-execution state root unavailable for block %d (%#x)", num, hash)
	}
}
