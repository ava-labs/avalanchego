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
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/x/blockdb"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	cchainstate "github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ hook.PointsG[*hookTx] = (*hooks)(nil)

type hooks struct {
	builder
	state       *cchainstate.State
	warpStorage *warp.Storage
}

func newHooks(
	ctx *snow.Context,
	state *cchainstate.State,
	chainConfig *ethparams.ChainConfig,
	pool *txpool.Pending,
	warpStorage *warp.Storage,
	now func() time.Time,
	desired desiredParams,
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
			chainConfig,
			now,
			poolTxs,
			desired,
		},
		state,
		warpStorage,
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

	header := b.Header()
	headerExtra := customtypes.GetHeaderExtra(header)
	now := h.BlockTime(header)
	return &builder{
		h.ctx,
		h.chainConfig,
		func() time.Time {
			return now
		},
		slices.Values(txs),
		desiredParams{
			targetExponent: headerExtra.TargetExponent,
			priceExponent:  headerExtra.MinPriceExponent,
			delayExponent:  (*dynamic.DelayExponent)(headerExtra.MinDelayExcess),
		},
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

// priceExponent returns h's ACP-283 price exponent, defaulting to
// [dynamic.InitialPriceExponent] when the header does not carry one.
func priceExponent(h *types.Header) dynamic.PriceExponent {
	if pe := customtypes.GetHeaderExtra(h).MinPriceExponent; pe != nil {
		return *pe
	}
	return dynamic.InitialPriceExponent
}

// delayExponent returns h's ACP-226 minimum block delay exponent, defaulting to
// [dynamic.InitialDelayExponent] when the header does not carry one.
func delayExponent(h *types.Header) dynamic.DelayExponent {
	if de := customtypes.GetHeaderExtra(h).MinDelayExcess; de != nil {
		return dynamic.DelayExponent(*de)
	}
	return dynamic.InitialDelayExponent
}

func targetExponent(config *extras.ChainConfig, h *types.Header) (dynamic.TargetExponent, error) {
	if te := customtypes.GetHeaderExtra(h).TargetExponent; te != nil {
		return *te, nil
	}
	if !config.IsFortuna(h.Time) || h.Number.Sign() == 0 {
		return dynamic.InitialTargetExponent, nil
	}

	// The block might be the last synchronous block running with ACP-176.
	state, err := acp176.ParseState(h.Extra)
	if err != nil {
		return 0, fmt.Errorf("parsing fee state: %w", err)
	}
	return dynamic.TargetExponent(state.TargetExcess), nil
}

func (h *hooks) GasConfigAfter(header *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	config := corethparams.GetExtra(h.chainConfig)
	te, err := targetExponent(config, header)
	if err != nil {
		te = dynamic.InitialTargetExponent
		h.ctx.Log.Error("failed to get target exponent; defaulting to the initial target exponent",
			zap.Stringer("blockHash", header.Hash()),
			zap.Uint64("blockNumber", header.Number.Uint64()),
			zap.Uint64("defaultTargetExponent", uint64(te)),
			zap.Uint64("defaultGasTarget", uint64(te.Target())),
			zap.Error(err),
		)
	}

	return te.Target(), gastime.GasPriceConfig{
		TargetToExcessScaling: 87, // 87 ~= 60 / ln(2)
		MinPrice:              priceExponent(header).Price(),
	}
}

func (*hooks) SettledBy(h *types.Header) hook.Settled {
	he := customtypes.GetHeaderExtra(h)
	if he.SettledHeight == nil ||
		he.SettledGasUnix == nil ||
		he.SettledGasNumerator == nil ||
		he.SettledExcess == nil {
		return hook.Settled{}
	}
	return hook.Settled{
		Height:       *he.SettledHeight,
		GasUnix:      *he.SettledGasUnix,
		GasNumerator: gas.Gas(*he.SettledGasNumerator),
		Excess:       gas.Gas(*he.SettledExcess),
	}
}

// BlockTime returns the canonical wall-clock time of a block.
//
// The whole-second value is authoritative and the millisecond field only
// refines it below the second, so the two can never disagree on which second a
// block belongs to. This keeps a block's time stable even when a peer sends a
// header whose millisecond field is inconsistent with its seconds.
func (*hooks) BlockTime(h *types.Header) time.Time {
	return blockTime(h)
}

func blockTime(h *types.Header) time.Time {
	ms := customtypes.HeaderTimeMilliseconds(h)
	subSecondNanos := int64(ms%1000) * int64(time.Millisecond) //#nosec G115 -- ms%1000 < 1000
	return time.Unix(int64(h.Time), subSecondNanos)            //#nosec G115 -- Won't overflow for a few millennia
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

func (*hooks) AfterExecutingTransaction(db *state.StateDB, baseFee uint256.Int, _ *types.Transaction, r *types.Receipt) error {
	var burned uint256.Int
	burned.SetUint64(r.GasUsed)
	burned.Mul(&burned, &baseFee)
	db.AddBalance(constants.BlackholeAddr, &burned)
	return nil
}

func (*hooks) BeforeExecutingBlock(ethparams.Rules, *state.StateDB, *types.Block) error {
	// TODO(StephenButtolph): If the genesis was configured to be pre-Durango
	// and this block is the first post-Durango block, we need to activate the
	// Warp precompile. This case does not happen on Mainnet, Fuji, or the Local
	// network, but could happen on a custom network.
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

	messages, err := warp.FromReceipts(receipts)
	if err != nil {
		return fmt.Errorf("parsing warp messages from receipts: %w", err)
	}
	if err := h.warpStorage.Add(messages...); err != nil {
		return fmt.Errorf("storing warp messages from receipts: %w", err)
	}
	return nil
}

var _ hook.BlockBuilder[*hookTx] = (*builder)(nil)

type builder struct {
	ctx         *snow.Context
	chainConfig *ethparams.ChainConfig

	now          func() time.Time
	potentialTxs iter.Seq[*hookTx]
	desired      desiredParams
}

var (
	errHeliconUnactivated = errors.New("helicon is not activated")
	errBelowMinBlockDelay = errors.New("block time below the ACP-226 minimum block delay")
)

// See [hook.BlockBuilder.BuildHeader] for which fields MUST or MAY be set in
// the returned header.
func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	now := b.now()
	if !b.ctx.NetworkUpgrades.IsHeliconActivated(now) {
		return nil, errHeliconUnactivated
	}

	de := delayExponent(parent)
	parentTime := blockTime(parent)
	if minTime := parentTime.Add(de.DelayDuration()); now.Before(minTime) {
		return nil, fmt.Errorf("%w: block time %s is before the minimum %s (parent %s + %s)",
			errBelowMinBlockDelay, now, minTime, parentTime, de.DelayDuration())
	}

	nowMS := uint64(now.UnixMilli()) //#nosec G115 -- Known non-negative

	// Enforce block-building separation against the parent's MinDelayExcess.
	{
		parentTimeMS := customtypes.HeaderTimeMilliseconds(parent)
		if nowMS < parentTimeMS {
			return nil, fmt.Errorf("current time is before parent timestamp: now=%d parentTime=%d", nowMS, parentTimeMS)
		}

		delay := nowMS - parentTimeMS
		minDelay := de.Delay()
		if delay < minDelay {
			return nil, fmt.Errorf("block building separation not satisfied: delay=%d minDelay=%d", delay, minDelay)
		}
	}

	config := corethparams.GetExtra(b.chainConfig)
	te, err := targetExponent(config, parent)
	if err != nil {
		return nil, fmt.Errorf("getting target exponent: %w", err)
	}
	// Move each dynamic parameter toward this node's vote (nil = no move).
	de = de.Toward(b.desired.delayExponent)
	te = te.Toward(b.desired.targetExponent)
	pe := priceExponent(parent).Toward(b.desired.priceExponent)
	minDelayExcess := acp226.DelayExcess(de)
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash:       parent.Hash(),
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             nowMS / 1000,
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
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: &nowMS,
			MinDelayExcess:   &minDelayExcess,
			TargetExponent:   &te,
			MinPriceExponent: &pe,
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
	_ context.Context,
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

var errEmptyBlock = errors.New("empty block")

func (b *builder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	ethTxs []*types.Transaction,
	receipts []*types.Receipt,
	avaxTxs []*hookTx,
	settled hook.Settled,
) (*types.Block, error) {
	if len(ethTxs) == 0 && len(avaxTxs) == 0 {
		return nil, errEmptyBlock
	}

	txs := make([]*tx.Tx, len(avaxTxs))
	for i, avaxTx := range avaxTxs {
		txs[i] = avaxTx.tx
	}
	extData, err := tx.MarshalSlice(txs)
	if err != nil {
		return nil, fmt.Errorf("marshalling txs: %w", err)
	}

	rules := b.chainConfig.Rules(header.Number, corethparams.IsMergeTODO, header.Time)
	rulesExtra := corethparams.GetRulesExtra(rules)
	warpValidity, err := warp.VerifyBlock(b.ctx, blockCtx, rulesExtra, ethTxs)
	if err != nil {
		return nil, fmt.Errorf("verifying warp messages: %w", err)
	}

	// TODO(StephenButtolph): Replace the predicate bytes format with an
	// efficiently packed canoto message. The current format is extremely
	// inefficient. There are 6 bytes of constant overhead, along with
	// unnecessarily including the contract address and tx hash. The warp
	// contract address is a constant, and the tx hash should be replaced with
	// the tx index.
	warpValidityBytes, err := warpValidity.Bytes()
	if err != nil {
		return nil, fmt.Errorf("serializing warp validity: %w", err)
	}
	header.Extra = warpValidityBytes

	// Encode the settled block marker into the header so [hooks.SettledBy] can recover it.
	he := customtypes.GetHeaderExtra(header)
	he.SettledHeight = &settled.Height
	he.SettledGasUnix = &settled.GasUnix
	he.SettledGasNumerator = (*uint64)(&settled.GasNumerator)
	he.SettledExcess = (*uint64)(&settled.Excess)

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
