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
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
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

// isSynchronous reports whether the block at the given height is at or below the
// synchronous frontier, i.e. it predates SAE and was executed by coreth.
func (b *backend) isSynchronous(height uint64) bool {
	return height <= b.LastSynchronous().NumberU64()
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

	numOrHash.RequireCanonical = true
	num, hash, err := blocks.ResolveRPCNumberOrHash(b, numOrHash)
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
	if bl, ok := b.ConsensusCriticalBlock(hash); ok {
		hdr = bl.Header()
		hdr.Root = bl.PostExecutionStateRoot()
		hdr.BaseFee = bl.ExecutedBaseFee().ToBig()
	} else {
		hdr = rawdb.ReadHeader(b.DB(), hash, num)

		// TODO(StephenButtolph): hdr may be nil after we support state sync.
		if !b.isSynchronous(hdr.Number.Uint64()) {
			// TODO(arr4n) export [blocks.executionResults] to avoid multiple
			// database reads and canoto unmarshallings here.
			var err error
			hdr.Root, err = blocks.PostExecutionStateRoot(b.XDB(), num)
			if err != nil {
				return nil, nil, err
			}

			bf, err := blocks.ExecutionBaseFee(b.XDB(), num)
			if err != nil {
				return nil, nil, err
			}
			hdr.BaseFee = bf.ToBig()
		}
	}

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
	var root common.Hash
	if b.isSynchronous(block.NumberU64()) {
		// If the block is synchronous, we can trust its post-execution state root.
		root = block.Root()
	} else {
		// Otherwise, we need to look up the post-execution state root for the
		// block, which may be on disk if the block is non-canonical.
		var err error
		root, err = b.postExecutionStateRoot(block.Hash(), block.NumberU64())
		if err != nil {
			return nil, nil, err
		}
	}

	sdb, err := b.StateDB(root)
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
// Pre-Helicon blocks were executed by coreth and replay under coreth's
// historical rules ([stateAtTransactionPreSAE]) while post-Helicon blocks replay
// under SAE rules ([stateAtTransactionSAE]).
//
// The Post-Helicon replay path calls [saexec.Execute] - the same pipeline used
// by [saexec.Executor] - with [noEndOfBlockOps] to suppress end-of-block
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

	// Pre-Helicon blocks predate SAE and replay under coreth's rules instead.
	if b.isSynchronous(ethB.NumberU64()) {
		parent := rawdb.ReadBlock(b.DB(), ethB.ParentHash(), ethB.NumberU64()-1)
		if parent == nil {
			return nil, bCtx, nil, nil, fmt.Errorf("parent block %d (%#x) of synchronous block %d not found", ethB.NumberU64()-1, ethB.ParentHash(), ethB.NumberU64())
		}
		return stateAtTransactionPreSAE(b, b.ChainContext(), b.ChainConfig(), ethB, parent.Root(), txIndex)
	}
	return b.stateAtTransactionSAE(ethB, txIndex)
}

// stateAtTransactionSAE returns the execution environment of the transaction at
// txIndex within a post-Helicon (SAE) block, replaying the preceding
// transactions via [saexec.Execute] with [noEndOfBlockOps] and
// [saexec.NullReceiptStore] to skip end-of-block operations and receipt
// broadcasting.
//
// txIndex MUST be in range; [backend.StateAtTransaction] validates it.
func (b *backend) stateAtTransactionSAE(ethB *types.Block, txIndex int) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	var bCtx vm.BlockContext
	if b.LastExecuted().NumberU64() < ethB.NumberU64()-1 {
		return nil, bCtx, nil, nil, fmt.Errorf("parent of block %d not executed yet", ethB.NumberU64())
	}
	parent, err := b.NewBlock(
		// The I(E) check above guarantees D(A) of the same block; see
		// ../docs/invariants.md for details.
		rawdb.ReadBlock(b.DB(), ethB.ParentHash(), ethB.NumberU64()-1),
		// Ancestry is irrelevant for the parent as we just want its
		// post-execution artefacts.
		nil, nil,
	)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing parent block: %v", err)
	}
	if b.isSynchronous(parent.NumberU64()) {
		// Synchronous block artefacts are header-derived, as in
		// [backend.StateAtBlock].
		if err := parent.RestoreSynchronousExecutionArtefacts(b.Hooks()); err != nil {
			return nil, bCtx, nil, nil, fmt.Errorf("parent %T.RestoreSynchronousExecutionArtefacts(...): %v", parent, err)
		}
	} else if err := parent.RestoreExecutionArtefacts(b.DB(), b.XDB(), b.ChainConfig()); err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("parent %T.RestoreExecutionArtefacts(...): %v", parent, err)
	}

	block, err := b.NewBlock(ethB, parent, nil)
	if err != nil {
		return nil, bCtx, nil, nil, fmt.Errorf("constructing SAE block: %v", err)
	}

	// Replay transactions 0..txIndex-1 to produce the state just before the
	// target transaction.
	result, err := saexec.Execute(
		block,
		b,
		txIndex,
		noEndOfBlockOps{b.Hooks()},
		b.ChainConfig(),
		b.ChainContext(),
		&saexec.NullReceiptStore{},
		b.Logger(),
	)
	if err != nil {
		return nil, bCtx, nil, nil, err
	}

	msg, err := core.TransactionToMessage(ethB.Transactions()[txIndex], result.Signer, result.BaseFee.ToBig())
	if err != nil {
		return nil, bCtx, nil, nil, err
	}
	return msg, result.BlockCtx, result.StateDB, noopRelease, nil
}

// stateAtTransactionPreSAE returns the execution environment of the transaction
// at txIndex within a pre-Helicon block, replaying the preceding transactions
// under coreth's historical execution rules. Each transaction is charged at the
// block's header base fee, not a value derived from the SAE gas clock.
//
// The recompute loop here is a near-verbatim copy of coreth's
// (*Ethereum).stateAtTransaction in graft/coreth/eth/state_accessor.go.
//
// TODO(JonathanOppenheimer): once coreth's stateAtTransaction is deleted, include
// a permalink to its final resting place.
func stateAtTransactionPreSAE(
	opener saedb.StateDBOpener,
	chainCtx core.ChainContext,
	cfg *params.ChainConfig,
	ethB *types.Block,
	parentRoot common.Hash,
	txIndex int,
) (*core.Message, vm.BlockContext, *state.StateDB, tracers.StateReleaseFunc, error) {
	statedb, err := opener.StateDB(parentRoot)
	if err != nil {
		return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("opening pre-Helicon parent state at %#x (tracing pre-Helicon blocks requires an archival node): %w", parentRoot, err)
	}
	release := noopRelease
	// SAE's ChainContext has no consensus engine, so the coinbase is supplied as
	// the block author explicitly (coreth passes nil and derives it via Author).
	coinbase := ethB.Coinbase()

	// Recompute transactions up to the target index.
	signer := types.MakeSigner(cfg, ethB.Number(), ethB.Time())
	for idx, tx := range ethB.Transactions() {
		// Assemble the transaction call message and return if the requested offset
		msg, _ := core.TransactionToMessage(tx, signer, ethB.BaseFee())
		txContext := core.NewEVMTxContext(msg)
		context := core.NewEVMBlockContext(ethB.Header(), chainCtx, &coinbase)
		if idx == txIndex {
			return msg, context, statedb, release, nil
		}
		// Not yet the searched for transaction, execute on top of the current state
		vmenv := vm.NewEVM(context, txContext, statedb, cfg, vm.Config{})
		statedb.SetTxContext(tx.Hash(), idx)
		if _, err := core.ApplyMessage(vmenv, msg, new(core.GasPool).AddGas(tx.Gas())); err != nil {
			return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction %#x failed: %v", tx.Hash(), err)
		}
		// Ensure any modifications are committed to the state
		// Only delete empty objects if EIP158/161 (a.k.a Spurious Dragon) is in effect
		statedb.Finalise(vmenv.ChainConfig().IsEIP158(ethB.Number()))
	}
	return nil, vm.BlockContext{}, nil, nil, fmt.Errorf("transaction index %d out of range for block %#x", txIndex, ethB.Hash())
}

// postExecutionStateRoot returns the post-execution state root for the block
// identified by hash and number, checking in-memory blocks first, then falling
// back to disk.
func (b *backend) postExecutionStateRoot(hash common.Hash, num uint64) (common.Hash, error) {
	switch bl, ok := b.ConsensusCriticalBlock(hash); {
	case !ok:
		return blocks.PostExecutionStateRoot(b.XDB(), num)
	case bl.Executed():
		return bl.PostExecutionStateRoot(), nil
	default:
		return common.Hash{}, fmt.Errorf("post-execution state root unavailable for block %d (%#x)", num, hash)
	}
}
