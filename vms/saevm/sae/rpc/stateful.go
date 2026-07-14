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
	"github.com/ava-labs/libevm/rpc"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
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
	if n, ok := numOrHash.Number(); ok && n == rpc.PendingBlockNumber {
		return nil, nil, errors.New("state not available for pending block")
	}

	bl, err := b.restoreBlock(numOrHash)
	if err != nil {
		return nil, nil, err
	}

	// The API implementations expect this to be synchronous, sourcing the state
	// root and the base fee from fields. At the time of writing, the returned
	// header's hash is never used so it's safe to modify it.
	//
	// TODO(arr4n) the above assumption is brittle under geth/libevm updates;
	// devise an approach to ensure that it is confirmed on each.
	hdr := bl.Header()
	hdr.Root = bl.PostExecutionStateRoot()
	hdr.BaseFee = bl.ExecutedBaseFee().ToBig()

	sdb, err := b.StateDB(hdr.Root)
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
//
//nolint:revive // General-purpose types lose the meaning of args if unused ones are removed
func (b *backend) StateAtBlock(ctx context.Context, block *types.Block, reexec uint64, base *state.StateDB, readOnly bool, preferDisk bool) (*state.StateDB, tracers.StateReleaseFunc, error) {
	bl, err := b.restoreBlock(rpc.BlockNumberOrHashWithHash(block.Hash(), true /* canonical */))
	if err != nil {
		return nil, nil, err
	}

	sdb, err := b.StateDB(bl.PostExecutionStateRoot())
	if err != nil {
		return nil, nil, err
	}
	return sdb, noopRelease, nil
}

// replay begins a re-execution of ethB's transactions from its parent's
// post-execution state with the given base fee (see [saexec.NewExecution]),
// suppressing receipt broadcasting. End-of-block operations never run because
// RPC callers MUST NOT call [saexec.Execution.Finish].
func (b *backend) replay(ethB *types.Block, baseFee *uint256.Int) (*saexec.Execution, error) {
	parent, err := b.restoreBlock(rpc.BlockNumberOrHashWithHash(ethB.ParentHash(), true /* canonical */))
	if err != nil {
		return nil, fmt.Errorf("restoring parent block: %w", err)
	}
	block, err := b.NewBlock(ethB, parent, nil)
	if err != nil {
		return nil, fmt.Errorf("constructing SAE block: %v", err)
	}
	return saexec.NewExecution(
		block,
		b,
		b.Hooks(),
		b.ChainConfig(),
		b.ChainContext(),
		&saexec.NullReceiptStore{},
		b.Logger(),
		baseFee,
	)
}

// StateAtTransaction returns the execution environment of a particular
// transaction within a block, replaying all preceding transactions with
// [backend.replay].
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

	// The gas clock cannot reproduce a synchronous block's base fee, so
	// restorable blocks replay with their originally executed one.
	var baseFee *uint256.Int
	switch target, err := b.restoreBlock(rpc.BlockNumberOrHashWithHash(ethB.Hash(), true /* canonical */)); {
	case err == nil:
		baseFee = target.ExecutedBaseFee()
	case !errors.Is(err, blocks.ErrNotFound):
		return nil, bCtx, nil, nil, fmt.Errorf("restoring block: %w", err)
	}

	exec, err := b.replay(ethB, baseFee)
	if err != nil {
		return nil, bCtx, nil, nil, err
	}
	for range txIndex {
		if _, err := exec.ExecuteNextTransaction(vm.Config{}); err != nil {
			return nil, bCtx, nil, nil, err
		}
	}

	msg, err := core.TransactionToMessage(txs[txIndex], exec.Signer(), exec.BaseFee().ToBig())
	if err != nil {
		return nil, bCtx, nil, nil, err
	}
	return msg, exec.BlockContext(), exec.StateDB(), noopRelease, nil
}
