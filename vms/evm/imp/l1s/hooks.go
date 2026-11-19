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
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/acp226"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/gastime"
	"github.com/ava-labs/avalanchego/vms/evm/hook"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/dynamic"
	"github.com/ava-labs/avalanchego/vms/evm/imp/executionresults"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/hookadaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/metrics"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeheader"

	legacycore "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	legacyparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	legacywarp "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
	saetypes "github.com/ava-labs/avalanchego/vms/evm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ hookadaptor.Points = (*hooks)(nil)

// hooks are the L1-specific [hookadaptor.Points].
type hooks struct {
	builder
	warpStorage *saewarp.Storage
	metrics     *metrics.Metrics
}

func newHooks(
	ctx *snow.Context,
	chainConfig *ethparams.ChainConfig,
	now func() time.Time,
	desired saeconfig.Desired,
	warpStorage *saewarp.Storage,
	coinbase common.Address,
	metrics *metrics.Metrics,
) *hooks {
	return &hooks{
		builder: builder{
			ctx:         ctx,
			chainConfig: chainConfig,
			now:         now,
			desired:     desired,
			coinbase:    coinbase,
			building:    true,
		},
		warpStorage: warpStorage,
		metrics:     metrics,
	}
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hookadaptor.BlockBuilder, error) {
	header := b.Header()
	headerExtra := customtypes.GetHeaderExtra(header)
	return &builder{
		ctx:         h.ctx,
		chainConfig: h.chainConfig,
		now: func() time.Time {
			return h.BlockTime(header)
		},
		desired: saeconfig.Desired{
			TargetExponent: headerExtra.TargetExponent,
			DelayExponent:  (*dynamic.DelayExponent)(headerExtra.MinDelayExcess),
		},
		coinbase: header.Coinbase,
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	return executionresults.New(dataDir, h.ctx.Log)
}

func (*hooks) GasConfigAfter(hdr *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	target := targetExponent(hdr).Target()
	he := customtypes.GetHeaderExtra(hdr)
	if he.TargetToExcessScaling == nil || he.MinPrice == nil { // Before SAE
		return target, gastime.DefaultGasPriceConfig()
	}
	c := gastime.GasPriceConfig{
		TargetToExcessScaling: gas.Gas(*he.TargetToExcessScaling),
		MinPrice:              gas.Price(*he.MinPrice),
	}
	// Static pricing ignores the scaling, but gastime requires a non-zero one.
	if c.TargetToExcessScaling == 0 {
		c.TargetToExcessScaling = gastime.DefaultTargetToExcessScaling
		c.StaticPricing = true
	}
	return target, c
}

func targetExponent(hdr *types.Header) dynamic.TargetExponent {
	if te := customtypes.GetHeaderExtra(hdr).TargetExponent; te != nil {
		return *te
	}
	return dynamic.InitialTargetExponent
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

// CanExecuteTransaction enforces the tx allow list.
func (*hooks) CanExecuteTransaction(rules ethparams.Rules, from common.Address, state libevm.StateReader) error {
	extra := legacyparams.GetRulesExtra(rules)
	if !extra.IsPrecompileEnabled(txallowlist.ContractAddress) {
		return nil
	}
	if role := txallowlist.GetTxAllowListStatus(state, from); !role.IsEnabled() {
		return fmt.Errorf("%w: %s", vmerrors.ErrSenderAddressNotAllowListed, from)
	}
	return nil
}

// StartExecutingBlock applies the precompile and state upgrades scheduled
// since parent, so each is applied exactly once.
func (h *hooks) StartExecutingBlock(_ ethparams.Rules, statedb *state.StateDB, parent *types.Header, block *types.Block) error {
	blockContext := legacycore.NewBlockContext(
		block.Number(),
		block.Time(),
	)
	return legacycore.ApplyUpgrades(
		h.chainConfig,
		&parent.Time,
		blockContext,
		statedb,
	)
}

func (*hooks) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

func (h *hooks) AfterExecutingBlock(b *types.Block, receipts types.Receipts) error {
	header := b.Header()
	if mde := customtypes.GetHeaderExtra(header).MinDelayExcess; mde != nil {
		h.metrics.SetMinBlockDelay(dynamic.DelayExponent(*mde).DelayDuration())
	}

	// Genesis and state upgrades can place arbitrary code at the Warp address.
	// Only once the precompile is active are its logs guaranteed to be Warp
	// messages.
	if _, ok := h.activeWarpConfig(header); !ok {
		return nil
	}
	messages, err := saewarp.FromReceipts(receipts)
	if err != nil {
		return fmt.Errorf("parsing warp messages from receipts: %w", err)
	}
	if err := h.warpStorage.Add(messages...); err != nil {
		return fmt.Errorf("storing warp messages from receipts: %w", err)
	}
	return nil
}

// activeWarpConfig returns the Warp precompile config and true if the
// precompile is active at header.
func (b *builder) activeWarpConfig(header *types.Header) (*legacywarp.Config, bool) {
	rules := b.chainConfig.Rules(header.Number, true /*isMerge*/, header.Time)
	rulesExtra := legacyparams.GetRulesExtra(rules)
	config := rulesExtra.Precompiles[legacywarp.ContractAddress]
	warpConfig, ok := config.(*legacywarp.Config)
	return warpConfig, ok
}

var _ hookadaptor.BlockBuilder = (*builder)(nil)

type builder struct {
	ctx         *snow.Context
	chainConfig *ethparams.ChainConfig

	now     func() time.Time
	desired saeconfig.Desired

	// coinbase is the operator's fee recipient. A rebuilder uses the received
	// block's coinbase so the rebuilt header hashes identically.
	coinbase common.Address
	// building is false for a rebuilder.
	building bool
}

var (
	errIglooUnactivated  = errors.New("igloo is not activated")
	errBlockTimeTooEarly = errors.New("block time too early")
)

// earliestBlockTime returns the earliest time a child of parent may be built.
func earliestBlockTime(parent *types.Header) time.Time {
	t := blockTime(parent)
	e := customtypes.GetHeaderExtra(parent)
	if e.MinDelayExcess == nil {
		return t
	}
	return t.Add(dynamic.DelayExponent(*e.MinDelayExcess).DelayDuration())
}

func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	now := b.now()
	configExtra := legacyparams.GetExtra(b.chainConfig)
	nowUnix := uint64(now.Unix()) //#nosec G115 -- block times are known non-negative
	if !configExtra.IsIgloo(nowUnix) {
		return nil, errIglooUnactivated
	}
	if earliest := earliestBlockTime(parent); now.Before(earliest) {
		return nil, fmt.Errorf("%w: %s is before %s", errBlockTimeTooEarly, now, earliest)
	}
	nowMS := uint64(now.UnixMilli()) //#nosec G115 -- block times are known non-negative

	mde := dynamic.InitialDelayExponent
	if pmde := customtypes.GetHeaderExtra(parent).MinDelayExcess; pmde != nil {
		mde = dynamic.DelayExponent(*pmde)
	}
	minDelayExcess := acp226.DelayExcess(mde.Toward(b.desired.DelayExponent))

	te := targetExponent(parent).Toward(b.desired.TargetExponent)
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash:       parent.Hash(),
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             nowUnix,
			BlobGasUsed:      new(uint64(0)),
			ExcessBlobGas:    new(uint64(0)),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			BlockGasCost:     big.NewInt(0), // Unused under SAE.
			TimeMilliseconds: new(nowMS),
			MinDelayExcess:   &minDelayExcess,
			TargetExponent:   &te,
		},
	), nil
}

var errEmptyBlock = errors.New("empty block")

func (b *builder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	settled hook.Settled,
	settledRules ethparams.Rules,
	worstCaseState libevm.StateReader,
) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}

	// A precompile's storage in worstCaseState is only initialized once its
	// activation has settled, so the rules at header.Time could read it
	// uninitialized.
	settledRulesExtra := legacyparams.GetRulesExtra(settledRules)
	header.Coinbase = b.resolveCoinbase(settledRulesExtra, worstCaseState)

	// Written even while gaspricemanager is disabled, because header fields
	// can't be removed once populated.
	headerExtra := customtypes.GetHeaderExtra(header)
	setGasConfig(headerExtra, readGasConfig(settledRulesExtra, worstCaseState))

	var warpValidity predicate.BlockResults
	if warpConfig, ok := b.activeWarpConfig(header); ok {
		var err error
		warpValidity, err = saewarp.VerifyBlock(b.ctx, blockCtx, warpConfig, txs)
		if err != nil {
			return nil, fmt.Errorf("verifying warp messages: %w", err)
		}
	}
	warpValidityBytes, err := warpValidity.Bytes()
	if err != nil {
		return nil, fmt.Errorf("serializing warp validity: %w", err)
	}
	header.Extra = customheader.SetPredicateBytesInExtra(header.Extra, warpValidityBytes)

	headerExtra.SettledHeight = &settled.Height
	headerExtra.SettledGasUnix = &settled.GasUnix
	headerExtra.SettledGasNumerator = (*uint64)(&settled.GasNumerator)
	headerExtra.SettledExcess = (*uint64)(&settled.Excess)

	return types.NewBlock(
		header,
		txs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
	), nil
}

// resolveCoinbase returns the fee recipient of the block being built.
func (b *builder) resolveCoinbase(rules *extras.Rules, state libevm.StateReader) common.Address {
	if rules.IsPrecompileEnabled(rewardmanager.ContractAddress) {
		if addr, isDynamic := rewardmanager.GetStoredRewardAddress(state); !isDynamic {
			return addr
		}
	} else if e := legacyparams.GetExtra(b.chainConfig); !e.AllowFeeRecipients {
		return blackhole.Address
	}

	if b.building && b.coinbase == blackhole.Address {
		b.ctx.Log.Warn(`burning priority fees; set "fee-recipient" to claim them`)
	}
	return b.coinbase
}
