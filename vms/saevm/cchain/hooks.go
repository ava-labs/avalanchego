// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"slices"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/x/blockdb"

	cchainstate "github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

var _ hook.PointsG[*hookTx] = (*hooks)(nil)

type hooks struct {
	builder
	state *cchainstate.State
}

func newHooks(
	ctx *snow.Context,
	state *cchainstate.State,
	pool *txpool.Pending,
) *hooks {
	poolTxs := func(yield func(*hookTx) bool) {
		for t := range pool.Iter() {
			ht, err := newHookTx(t, ctx.AVAXAssetID)
			if err != nil {
				ctx.Log.Warn("failed to convert tx",
					zap.Stringer("txID", t.ID()),
					zap.Error(err),
				)
				continue
			}
			if !yield(ht) {
				return
			}
		}
	}
	return &hooks{
		builder{
			ctx,
			time.Now,
			poolTxs,
		},
		state,
	}
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*hookTx], error) {
	rawTxs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("parsing txs: %w", err)
	}

	txs := make([]*hookTx, len(rawTxs))
	for i, t := range rawTxs {
		ht, err := newHookTx(t, h.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("converting tx %s (%d): %w", t.ID(), i, err)
		}
		txs[i] = ht
	}

	now := h.BlockTime(b.Header())
	return &builder{
		h.ctx,
		func() time.Time {
			return now
		},
		slices.Values(txs),
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		h.ctx.Log,
	)
	if err != nil {
		return saetypes.ExecutionResults{}, fmt.Errorf("creating execution results db: %w", err)
	}
	return saetypes.ExecutionResults{
		HeightIndex: db,
	}, nil
}

func (*hooks) GasConfigAfter(*types.Header) (gas.Gas, gastime.GasPriceConfig) {
	// TODO(StephenButtolph): Extract parameters from the header.
	return 1_000_000, gastime.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
	}
}

func (*hooks) SettledBy(*types.Header) hook.Settled {
	// TODO(StephenButtolph): Extract from the header.
	return hook.Settled{}
}

func (*hooks) BlockTime(h *types.Header) time.Time {
	// TODO(StephenButtolph): Extract milliseconds from the header.
	return time.Unix(int64(h.Time), 0) //#nosec G115 -- Won't overflow for a few millennia
}

func (h *hooks) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("parsing txs: %w", err)
	}

	ops := make([]hook.Op, len(txs))
	for i, t := range txs {
		op, err := t.AsOp(h.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("converting tx %s (%d): %w", t.ID(), i, err)
		}
		ops[i] = op
	}
	return ops, nil
}

func (*hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (h *hooks) AfterExecutingBlock(statedb *state.StateDB, b *types.Block, receipts types.Receipts) error {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return fmt.Errorf("parsing txs: %w", err)
	}

	extstatedb := extstate.New(statedb)
	for i, t := range txs {
		if err := t.TransferNonAVAX(h.ctx.AVAXAssetID, extstatedb); err != nil {
			return fmt.Errorf("transferring non-AVAX assets of tx %s (%d): %w", t.ID(), i, err)
		}
	}

	if err := h.state.Apply(b.NumberU64(), txs); err != nil {
		return fmt.Errorf("applying cross-chain state: %w", err)
	}

	// TODO(StephenButtolph): Persist produced warp messages.
	_ = receipts
	return nil
}

var _ hook.BlockBuilder[*hookTx] = (*builder)(nil)

type builder struct {
	ctx          *snow.Context
	now          func() time.Time
	potentialTxs iter.Seq[*hookTx]
}

// See [hook.BlockBuilder.BuildHeader] for which fields MUST or MAY be set in
// the returned header.
func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	// TODO(StephenButtolph): Encode the ACP-176 target excess in the header.
	// TODO(StephenButtolph): Encode the ACP-183 min price excess in the header.
	// TODO(StephenButtolph): Enforce the minimum block time here.
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash:       parent.Hash(),
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(b.now().Unix()), //#nosec G115 -- Known non-negative
			BlobGasUsed:      new(uint64),
			ExcessBlobGas:    new(uint64),
			ParentBeaconRoot: new(common.Hash),
		},
		&customtypes.HeaderExtra{
			// Prior to SAE, ExtDataGasUsed included the gas cost of the
			// cross-chain transactions. However, with SAE, the gas cost is
			// included in [types.Header.GasUsed] with [hook.Op.Gas].
			ExtDataGasUsed: big.NewInt(0),
			// BlockGasCost has been set to 0 since the Granite upgrade.
			BlockGasCost: big.NewInt(0),
			// TODO(StephenButtolph): Encode the millisecond timestamp.
			TimeMilliseconds: new(uint64),
			// TODO(StephenButtolph): Encode the min-delay excess.
			MinDelayExcess: new(acp226.DelayExcess),
		},
	), nil
}

// PotentialEndOfBlockOps returns the cross-chain transactions that should be
// considered for inclusion in the block being built.
//
// This method MUST only return transactions that are valid to be accepted with
// respect to shared memory.
//
// SAE will perform additional checks on the transactions to ensure they are
// valid with respect to the worst-case state.
func (b *builder) PotentialEndOfBlockOps(
	ctx context.Context,
	building *types.Header,
	settledHash common.Hash,
	source saetypes.BlockSource,
) iter.Seq[*hookTx] {
	return func(yield func(*hookTx) bool) {
		// Transactions are verified against the last executed state. We must
		// also verify that they don't conflict with any transactions in blocks
		// between the block we are building and the last executed block. Since
		// we know the settled block has been executed, we use that as our
		// reference point.
		inputs, err := ancestorInputIDs(building, settledHash, source)
		if err != nil {
			b.ctx.Log.Error("failed to get ancestor input IDs",
				zap.Error(err),
			)
			return
		}

		for t := range b.potentialTxs {
			if inputs.Overlaps(t.inputs) {
				b.ctx.Log.Debug("tx consumes previously consumed inputs",
					zap.Stringer("txID", t.id),
				)
				continue
			}

			// Transactions in the txpool have already been sanity checked and
			// had their credentials verified, but we also process transactions
			// from blocks provided by peers here.
			if err := t.tx.SanityCheck(b.ctx); err != nil {
				b.ctx.Log.Debug("tx failed sanity check",
					zap.Stringer("txID", t.id),
					zap.Error(err),
				)
				continue
			}

			// Even for transactions from the txpool, which may have been
			// verified against out-dated state, we need to ensure that import
			// txs are consuming UTXOs that still exist so that our in-memory
			// UTXO conflict checks are sufficient.
			if err := t.tx.VerifyCredentials(b.ctx.SharedMemory); err != nil {
				b.ctx.Log.Debug("tx failed credential verification",
					zap.Stringer("txID", t.id),
					zap.Error(err),
				)
				continue
			}

			if !yield(t) {
				return
			}
			inputs.Union(t.inputs)
		}
	}
}

var errMissingBlock = errors.New("missing block")

// ancestorInputIDs returns the set of input IDs of all cross-chain transactions
// in the block range (h, settled), both exclusive.
func ancestorInputIDs(h *types.Header, settled common.Hash, source saetypes.BlockSource) (set.Set[ids.ID], error) {
	var s set.Set[ids.ID]
	for h.ParentHash != settled {
		parentNumber := h.Number.Uint64() - 1
		p, ok := source(h.ParentHash, parentNumber)
		if !ok {
			return nil, fmt.Errorf("%w: %s (%d)", errMissingBlock, h.ParentHash, parentNumber)
		}

		txs, err := tx.ParseSlice(customtypes.BlockExtData(p))
		if err != nil {
			return nil, fmt.Errorf("parsing txs: %s (%d): %w", h.ParentHash, parentNumber, err)
		}
		for _, t := range txs {
			s.Union(t.InputIDs())
		}
		h = p.Header()
	}
	return s, nil
}

func (*builder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	ethTxs []*types.Transaction,
	receipts []*types.Receipt,
	avaxTxs []*hookTx,
	settled hook.Settled,
) (*types.Block, error) {
	txs := make([]*tx.Tx, len(avaxTxs))
	for i, avaxTx := range avaxTxs {
		txs[i] = avaxTx.tx
	}
	extData, err := tx.MarshalSlice(txs)
	if err != nil {
		return nil, fmt.Errorf("marshalling txs: %w", err)
	}

	// TODO(StephenButtolph): Encode warp predicate results in the header.
	_ = blockCtx
	// TODO(StephenButtolph): Encode settled in the block.
	_ = settled
	return customtypes.NewBlockWithExtData(
		header,
		ethTxs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
		extData,
		true, // update [customtypes.HeaderExtra.ExtDataHash]
	), nil
}

var _ hook.Transaction = (*hookTx)(nil)

// hookTx adapts a [tx.Tx] to the [hook.Transaction] interface.
type hookTx struct {
	id     ids.ID
	tx     *tx.Tx
	inputs set.Set[ids.ID]
	op     hook.Op
}

func newHookTx(t *tx.Tx, avaxAssetID ids.ID) (*hookTx, error) {
	op, err := t.AsOp(avaxAssetID)
	if err != nil {
		return nil, err
	}
	return &hookTx{
		id:     op.ID,
		tx:     t,
		inputs: t.InputIDs(),
		op:     op,
	}, nil
}

func (t *hookTx) AsOp() hook.Op { return t.op }
