// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeheader"
	"github.com/ava-labs/avalanchego/x/blockdb"

	legacycore "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	l1params "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/evm/types"
)

var _ hookadaptor.Points = (*hooks)(nil)

// hooks are the L1-specific [hookadaptor.Points].
type hooks struct {
	builder
	chainConfig *params.ChainConfig
}

func newHooks(ctx *snow.Context, c *params.ChainConfig, now func() time.Time) *hooks {
	return &hooks{
		builder: builder{
			ctx: ctx,
			now: now,
		},
		chainConfig: c,
	}
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hookadaptor.BlockBuilder, error) {
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
	// config. Until then, use its default target.
	target := gas.Gas(commontype.DefaultGasPriceConfig().TargetGas)
	return target, gastime.GasPriceConfig{
		TargetToExcessScaling: gastime.DefaultTargetToExcessScaling,
		MinPrice:              gastime.DefaultMinPrice,
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
func (*hooks) BlockTime(h *types.Header) time.Time {
	return blockTime(h)
}

func blockTime(h *types.Header) time.Time {
	return saeheader.Time(h.Time, customtypes.HeaderTimeMilliseconds(h))
}

func (*hooks) VerifyBlockSyntax(*types.Block) error {
	// L1 VM has no extra block body fields.
	return nil
}

func (*hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	// TODO: Enforce the transaction allowlist precompile.
	return nil
}

// StartExecutingBlock applies all upgrades starting at b.
func (h *hooks) StartExecutingBlock(_ params.Rules, sdb *state.StateDB, parent *types.Header, b *types.Block) error {
	var (
		parentTime  = new(parent.Time)
		currentTime = b.Time()
		number      = b.Number()
	)

	if err := legacycore.ApplyPrecompileActivations(
		h.chainConfig,
		parentTime,
		legacycore.NewBlockContext(number, currentTime),
		sdb,
	); err != nil {
		return fmt.Errorf("applying precompile activations: %w", err)
	}

	configExtra := l1params.GetExtra(h.chainConfig)
	for _, upgrade := range configExtra.GetActivatingStateUpgrades(parentTime, currentTime) {
		upgrade.Apply(h.chainConfig, sdb, number)
	}

	return nil
}

func (*hooks) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

func (*hooks) AfterExecutingBlock(*types.Block, types.Receipts) error {
	// TODO: Store the Warp messages sent by the block.
	return nil
}

var _ hookadaptor.BlockBuilder = (*builder)(nil)

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
			Coinbase:         blackhole.Address,
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

var errEmptyBlock = errors.New("empty block")

func (*builder) BuildBlock(
	header *types.Header,
	_ *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
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
