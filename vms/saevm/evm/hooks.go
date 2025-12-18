// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/worstcase"
	"go.uber.org/zap"

	atomictxpool "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/txpool"
	atomicvm "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
)

const targetAtomicTxsSize = 40 * units.KiB

var (
	_ hook.Points = &hooks{}

	errEmptyBlock = errors.New("empty block")
)

// txs supports building blocks from a sequence of atomic transactions.
type txs interface {
	NextTx() (*atomic.Tx, bool)
	CancelCurrentTx(txID ids.ID)
	DiscardCurrentTx(txID ids.ID)
	DiscardCurrentTxs()
}

type hooks struct {
	ctx         *snow.Context
	chainConfig *params.ChainConfig
	mempool     *atomictxpool.Txs

	// TODO: Handle this correctly
	bootstrapped bool

	// TODO: Make this a global and initialize it
	fx secp256k1fx.Fx
	// TODO: Make this a global and initialize it
	cache *secp256k1.RecoverCache
}

func (h *hooks) GasTarget(parent *types.Block) gas.Gas {
	// TODO: implement me
	return acp176.MinTargetPerSecond
}

func (h *hooks) ConstructBlock(
	ctx context.Context,
	blockContext *block.Context,
	header *types.Header,
	parent *types.Header,
	ancestors iter.Seq[*types.Block],
	state hook.State,
	txs []*types.Transaction,
	receipts []*types.Receipt,
) (*types.Block, error) {
	return h.constructBlock(
		ctx,
		blockContext,
		header,
		parent,
		ancestors,
		state,
		txs,
		receipts,
		h.mempool,
	)
}

func (h *hooks) BlockExecuted(ctx context.Context, block *types.Block, receipts types.Receipts) error {
	// TODO: Write warp information
	// TODO: Apply atomic txs to shared memory
	// TODO: Update last executed height to support restarts
	return nil
}

func (h *hooks) ConstructBlockFromBlock(ctx context.Context, b *types.Block) (hook.ConstructBlock, error) {
	atomicTxs, err := atomic.ExtractAtomicTxs(
		customtypes.BlockExtData(b),
		true,
		atomic.Codec,
	)
	if err != nil {
		return nil, err
	}

	atomicTxSlice := txSlice(atomicTxs)
	return func(
		ctx context.Context,
		blockContext *block.Context,
		header *types.Header,
		parent *types.Header,
		ancestors iter.Seq[*types.Block],
		state hook.State,
		txs []*types.Transaction,
		receipts []*types.Receipt,
	) (*types.Block, error) {
		return h.constructBlock(
			ctx,
			blockContext,
			header,
			parent,
			ancestors,
			state,
			txs,
			receipts,
			&atomicTxSlice,
		)
	}, nil
}

func (h *hooks) constructBlock(
	ctx context.Context,
	blockContext *block.Context,
	header *types.Header,
	parent *types.Header,
	ancestors iter.Seq[*types.Block],
	state hook.State,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	potentialAtomicTxs txs,
) (*types.Block, error) {
	ancestorInputUTXOs, err := inputUTXOs(ancestors)
	if err != nil {
		return nil, err
	}

	rules := h.chainConfig.Rules(header.Number, params.IsMergeTODO, header.Time)
	rulesExtra := params.GetRulesExtra(rules)
	atomicTxs, err := packAtomicTxs(
		ctx,
		h.ctx,
		&h.fx,
		h.cache,
		rulesExtra,
		h.bootstrapped,
		state,
		header.BaseFee,
		ancestorInputUTXOs,
		potentialAtomicTxs,
	)
	if err != nil {
		return nil, err
	}

	// Blocks must either settle a prior transaction, include a new ethereum tx,
	// or include a new atomic tx.
	if header.GasUsed == 0 && len(txs) == 0 && len(atomicTxs) == 0 {
		return nil, errEmptyBlock
	}

	// TODO: This is where the block fee should be verified, do we still want to
	// utilize a block fee?

	atomicTxBytes, err := marshalAtomicTxs(atomicTxs)
	if err != nil {
		// If we fail to marshal the batch of atomic transactions for any
		// reason, discard the entire set of current transactions.
		h.ctx.Log.Debug("discarding txs due to error marshaling atomic transactions",
			zap.Error(err),
		)
		potentialAtomicTxs.DiscardCurrentTxs()
		return nil, fmt.Errorf("failed to marshal batch of atomic transactions due to %w", err)
	}

	// TODO: What should we be doing with the ACP-176 logic here?
	//
	// chainConfigExtra := params.GetExtra(h.chainConfig)
	// extraPrefix, err := customheader.ExtraPrefix(chainConfigExtra, parent, header, nil) // TODO: Populate desired target excess
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to calculate new header.Extra: %w", err)
	// }

	predicateResults, err := core.CheckBlockPredicates(
		rules,
		&precompileconfig.PredicateContext{
			SnowCtx:            h.ctx,
			ProposerVMBlockCtx: blockContext,
		},
		txs,
	)
	if err != nil {
		return nil, fmt.Errorf("CheckBlockPredicates: %w", err)
	}

	predicateResultsBytes, err := predicateResults.Bytes()
	if err != nil {
		return nil, fmt.Errorf("predicateResults bytes: %w", err)
	}

	header.Extra = predicateResultsBytes // append(extraPrefix, predicateResultsBytes...)
	return customtypes.NewBlockWithExtData(
		header,
		txs,
		nil,
		receipts,
		trie.NewStackTrie(nil),
		atomicTxBytes,
		true,
	), nil
}

func (h *hooks) ExtraBlockOperations(ctx context.Context, block *types.Block) ([]hook.Op, error) {
	txs, err := atomic.ExtractAtomicTxs(
		customtypes.BlockExtData(block),
		true,
		atomic.Codec,
	)
	if err != nil {
		return nil, err
	}

	baseFee := block.BaseFee()
	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := atomicTxOp(tx, h.ctx.AVAXAssetID, baseFee)
		if err != nil {
			return nil, err
		}
		ops[i] = op
	}
	return ops, nil
}

func packAtomicTxs(
	ctx context.Context,
	snowContext *snow.Context,
	fx *secp256k1fx.Fx,
	cache *secp256k1.RecoverCache,
	rules *extras.Rules,
	bootstrapped bool,
	state hook.State,
	baseFee *big.Int,
	ancestorInputUTXOs set.Set[ids.ID],
	txs txs,
) ([]*atomic.Tx, error) {
	var (
		cumulativeSize int
		atomicTxs      []*atomic.Tx
	)
	for {
		tx, exists := txs.NextTx()
		if !exists {
			break
		}

		// Ensure that adding [tx] to the block will not exceed the block size
		// soft limit.
		txSize := len(tx.SignedBytes())
		if cumulativeSize+txSize > targetAtomicTxsSize {
			txs.CancelCurrentTx(tx.ID())
			break
		}

		// VerifyTx ensures:
		// 1. Transactions are syntactically valid.
		// 2. Transactions do not produces more assets than they consume,
		//    including the fees.
		// 3. Inputs all have corresponding credentials with valid signatures.
		// 4. ImportTxs are consuming UTXOs that are currently in shared memory.
		err := atomicvm.VerifyTx(
			snowContext,
			*rules,
			fx,
			cache,
			bootstrapped,
			tx,
			baseFee,
		)
		if err != nil {
			txID := tx.ID()
			snowContext.Log.Debug("discarding tx due to failed verification",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			txs.DiscardCurrentTx(txID)
			continue
		}

		// Verify that any ImportTxs do not conflict with prior ImportTxs,
		// either in the same block or in an ancestor.
		inputUTXOs := tx.InputUTXOs()
		if ancestorInputUTXOs.Overlaps(inputUTXOs) {
			txID := tx.ID()
			snowContext.Log.Debug("discarding tx due to overlapping input utxos",
				zap.Stringer("txID", txID),
			)
			txs.DiscardCurrentTx(txID)
			continue
		}

		// The atomicTxOp will verify that ExportTxs have sufficient funds and
		// utilize proper nonces.
		op, err := atomicTxOp(tx, snowContext.AVAXAssetID, baseFee)
		if err != nil {
			txs.DiscardCurrentTx(tx.ID())
			continue
		}

		err = state.Apply(op)
		if errors.Is(err, worstcase.ErrBlockTooFull) || errors.Is(err, worstcase.ErrQueueTooFull) {
			// Send [tx] back to the mempool's tx heap.
			txs.CancelCurrentTx(tx.ID())
			break
		}
		if err != nil {
			txID := tx.ID()
			snowContext.Log.Debug("discarding tx from mempool due to failed verification",
				zap.Stringer("txID", txID),
				zap.Error(err),
			)
			txs.DiscardCurrentTx(txID)
			continue
		}

		atomicTxs = append(atomicTxs, tx)
		ancestorInputUTXOs.Union(inputUTXOs)

		cumulativeSize += txSize
	}
	return atomicTxs, nil
}
