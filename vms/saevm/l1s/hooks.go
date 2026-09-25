// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/x/blockdb"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

var _ hook.PointsG[hook.Transaction] = (*hooks)(nil)

// hooks are the L1-specific [hook.PointsG].
type hooks struct {
	builder
}

func newHooks(ctx *snow.Context, now func() time.Time) *hooks {
	return &hooks{
		builder{
			ctx: ctx,
			now: now,
		},
	}
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[hook.Transaction], error) {
	now := blockTime(b.Header())
	return &builder{
		ctx: h.ctx,
		now: func() time.Time {
			return now
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

func (*hooks) GasConfigAfter(*types.Header) (gas.Gas, gastime.GasPriceConfig) {
	// TODO: Derive the gas target and price config from the ACP-224 fee
	// config.
	return acp176.MinTargetPerSecond, gastime.GasPriceConfig{
		TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
		MinPrice:              gastime.DefaultMinPrice,
	}
}

func (*hooks) SettledBy(h *types.Header) hook.Settled {
	he := customtypes.GetHeaderExtra(h)
	return hook.NewSettled(he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess)
}

// BlockTime returns the canonical wall-clock time of a block.
func (*hooks) BlockTime(h *types.Header) time.Time {
	return blockTime(h)
}

func blockTime(h *types.Header) time.Time {
	return hook.BlockTime(h.Time, customtypes.HeaderTimeMilliseconds(h))
}

func (*hooks) VerifyBlockSyntax(*types.Block) error {
	// TODO: Verify the L1-specific header fields.
	return nil
}

// EndOfBlockOps returns no operations, as L1s have no transactions outside of
// the EVM.
func (*hooks) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

func (*hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	// TODO: Enforce the transaction allowlist precompile.
	return nil
}

func (*hooks) StartExecutingBlock(params.Rules, *state.StateDB, *types.Header, *types.Block) error {
	// TODO: Apply the scheduled precompile and state upgrades.
	return nil
}

func (*hooks) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

func (*hooks) AfterExecutingBlock(*types.Block, types.Receipts) error {
	// TODO: Store the Warp messages sent by the block.
	return nil
}

var _ hook.BlockBuilder[hook.Transaction] = (*builder)(nil)

type builder struct {
	ctx *snow.Context
	now func() time.Time
}

// See [hook.BlockBuilder.BuildHeader] for which fields MUST or MAY be set in
// the returned header.
func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	// TODO: Enforce the ACP-226 minimum block delay, and move it toward this
	// node's desired delay.
	minDelayExcess := acp226.InitialDelayExcess
	if de := customtypes.GetHeaderExtra(parent).MinDelayExcess; de != nil {
		minDelayExcess = *de
	}

	nowMS := uint64(b.now().UnixMilli()) //#nosec G115 -- Known non-negative
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: parent.Hash(),
			// TODO: Support fee recipients and the reward manager precompile.
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             nowMS / 1000,
			BlobGasUsed:      new(uint64),
			ExcessBlobGas:    new(uint64),
			ParentBeaconRoot: new(common.Hash),
		},
		&customtypes.HeaderExtra{
			// BlockGasCost has been set to 0 since the Granite upgrade.
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: &nowMS,
			MinDelayExcess:   &minDelayExcess,
		},
	), nil
}

// PotentialEndOfBlockOps returns no transactions, as L1s have no transactions
// outside of the EVM.
func (*builder) PotentialEndOfBlockOps(context.Context, *types.Header, common.Hash, saetypes.BlockSource) iter.Seq[hook.Transaction] {
	return func(func(hook.Transaction) bool) {}
}

var errEmptyBlock = errors.New("empty block")

func (*builder) BuildBlock(
	header *types.Header,
	_ *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []hook.Transaction,
	settled hook.Settled,
) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}

	// TODO: Include the Warp predicate results.

	// Encode the settled block marker into the header so [hooks.SettledBy] can recover it.
	he := customtypes.GetHeaderExtra(header)
	he.SettledHeight = &settled.Height
	he.SettledGasUnix = &settled.GasUnix
	he.SettledGasNumerator = (*uint64)(&settled.GasNumerator)
	he.SettledExcess = (*uint64)(&settled.Excess)

	return types.NewBlock(
		header,
		txs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
	), nil
}
