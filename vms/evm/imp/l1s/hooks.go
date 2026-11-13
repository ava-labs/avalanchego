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
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/vmerrors"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
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
			TargetExponent: (*dynamic.TargetExponent)(headerExtra.TargetExcess),
			DelayExponent:  (*dynamic.DelayExponent)(headerExtra.MinDelayExcess),
		},
		coinbase: header.Coinbase, // override with received block's Coinbase
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	return executionresults.New(dataDir, h.ctx.Log)
}

// GasConfigAfter derives the gas target and price config in effect after `h`
// purely from the header (plus, for the genesis block, the chain config):
//
//  1. `hdr` carries a gas-config group (see [stampGasConfig], stamped by
//     [builder.BuildBlock] whenever gaspricemanager is enabled at
//     the settled timestamp): the group is authoritative for price config.
//     `TargetExcess` is always the target authority; BuildBlock pins it
//     to the precompile target when validator target selection is disabled.
//  2. `hdr` is the genesis block (synchronously executed, so never stamped)
//     and gaspricemanager is enabled at genesis: the group is derived from
//     the chain config exactly as [gaspricemanager.Configure] seeded storage.
//  3. Otherwise ACP-176 defaults apply, with the target from `TargetExcess`.
func (h *hooks) GasConfigAfter(hdr *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	headerTarget := dynamic.TargetExponent(targetExcess(hdr)).Target()
	if cfg, ok := readGasConfig(customtypes.GetHeaderExtra(hdr)); ok {
		return headerTarget, cfg
	}
	if hdr.Number.Sign() == 0 {
		if stored, ok := h.genesisGasConfig(hdr.Time); ok {
			if !stored.ValidatorTargetGas {
				headerTarget = dynamic.DesiredTargetExponent(gas.Gas(stored.TargetGas)).Target()
			}
			return headerTarget, gasConfigFromStored(stored)
		}
	}
	return headerTarget, gastime.DefaultGasPriceConfig()
}

// genesisGasConfig reports the gas config seeded into genesis state when
// gaspricemanager is enabled at the genesis timestamp `genesisTime`,
// mirroring [gaspricemanager.Configure]. This is deterministic across nodes
// because it is a pure function of the chain config.
//
// Unlike stamped headers, this value is re-derived from code constants at
// read time: [commontype.DefaultGasPriceConfig] and [scalingFromTimeToDouble]
// are consensus-critical for chains whose genesis activates gaspricemanager,
// and changing them requires a network upgrade.
func (h *hooks) genesisGasConfig(genesisTime uint64) (commontype.GasPriceConfig, bool) {
	configExtra := legacyparams.GetExtra(h.chainConfig)
	if !configExtra.IsPrecompileEnabled(gaspricemanager.ContractAddress, genesisTime) {
		return commontype.GasPriceConfig{}, false
	}
	precompileConfig := configExtra.GetActivePrecompileConfig(gaspricemanager.ContractAddress, genesisTime)
	stored := commontype.DefaultGasPriceConfig()
	if cfg, ok := precompileConfig.(*gaspricemanager.Config); ok && cfg.InitialGasPriceConfig != nil {
		stored = *cfg.InitialGasPriceConfig
	}
	return stored, true
}

// targetExcess returns hdr's ACP-176 gas-target vote, defaulting to zero when
// the header does not carry one.
func targetExcess(hdr *types.Header) gas.Gas {
	if te := customtypes.GetHeaderExtra(hdr).TargetExcess; te != nil {
		return *te
	}
	return 0
}

// SettledBy returns the settlement marker encoded in the header by
// [builder.BuildBlock], or the zero value (indicating synchronous,
// pre-SAE execution) when any of the quartet is missing.
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

var (
	// errNonZeroBlockGasCost is returned by [hooks.VerifyBlockSyntax] for a
	// header whose BlockGasCost is neither nil nor zero: SAE always stamps
	// zero (ACP-226 superseded its use).
	errNonZeroBlockGasCost = errors.New("non-zero BlockGasCost under SAE")
	// errPartialSettledMarker is returned by [hooks.VerifyBlockSyntax] when
	// only some of the Settled* header fields are set; [hooks.SettledBy]
	// requires all-or-nothing.
	errPartialSettledMarker = errors.New("partially populated settled marker")
	// errPartialGasConfig is returned by [hooks.VerifyBlockSyntax] when only
	// some of the GasConfig* header fields are set; [readGasConfig] requires
	// all-or-nothing.
	errPartialGasConfig = errors.New("partially populated gas-config group")
)

// VerifyBlockSyntax checks the stateless L1-specific invariants of a
// parsed block: BlockGasCost is unused under SAE and MUST be zero, and the
// optional Settled* and GasConfig* header-extra groups MUST each be fully
// populated or fully absent.
//
// Note the all-or-nothing checks only reject suffix-truncated groups: RLP
// decodes a present-but-empty (0x80) optional item as a pointer to zero, not
// nil, so a crafted header can carry zero-valued group fields. That is safe —
// an honest stamp is never all-zero, so such a header fails the
// rebuild-hash-equality check in block verification.
func (*hooks) VerifyBlockSyntax(b *types.Block) error {
	he := customtypes.GetHeaderExtra(b.Header())
	if he.BlockGasCost != nil && he.BlockGasCost.Sign() != 0 {
		return fmt.Errorf("%w: %v", errNonZeroBlockGasCost, he.BlockGasCost)
	}

	settledFields := 0
	for _, f := range []*uint64{he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess} {
		if f != nil {
			settledFields++
		}
	}
	if settledFields != 0 && settledFields != 4 {
		return fmt.Errorf("%w: %d of 4 fields set", errPartialSettledMarker, settledFields)
	}

	gasConfigFields := 0
	for _, set := range []bool{
		he.GasConfigTargetToExcessScaling != nil,
		he.GasConfigMinGasPrice != nil,
		he.GasConfigStaticPricing != nil,
	} {
		if set {
			gasConfigFields++
		}
	}
	if gasConfigFields != 0 && gasConfigFields != 3 {
		return fmt.Errorf("%w: %d of 3 fields set", errPartialGasConfig, gasConfigFields)
	}
	return nil
}

// CanExecuteTransaction rejects transactions from senders excluded by the
// tx allow list, as of the provided rules and state.
//
// The deployer allow list is not checked. It is enforced during execution, so
// a disallowed deployment is included and fails instead of being rejected.
func (*hooks) CanExecuteTransaction(rules ethparams.Rules, from common.Address, _ *common.Address, state libevm.StateReader) error {
	extra := legacyparams.GetRulesExtra(rules)
	if !extra.IsPrecompileEnabled(txallowlist.ContractAddress) {
		return nil
	}
	if role := txallowlist.GetTxAllowListStatus(state, from); !role.IsEnabled() {
		return fmt.Errorf("%w: %s", vmerrors.ErrSenderAddressNotAllowListed, from)
	}
	return nil
}

// StartExecutingBlock activates / deactivates timestamp-scheduled
// `PrecompileUpgrades` and `StateUpgrades` for the window
// (parent.Time, block.Time()] by delegating to [legacycore.ApplyUpgrades].
//
// SAE's `saexec.Execute` does not call `core.StateProcessor.Process` (it loops
// `core.ApplyTransaction` from libevm directly), so this hook is the single
// place where upgrade activations enter the per-block flow. Mutations made
// here are committed into the block's post-execution state root.
//
// Uses `parent.Time` and `statedb` rooted at `parent.PostExecutionStateRoot()`
// -- NOT the lagged last-settled rules and state used by
// [builder.BuildBlock] and [hooks.CanExecuteTransaction]:
// `ApplyUpgrades` requires a contiguous (parentTimestamp, blockTimestamp]
// activation window, and the live `statedb` must carry parent's full
// post-exec mutations into the upcoming `core.ApplyTransaction` loop. The
// build/admit-time worst-case path tolerates the Tau lag; this post-Tau
// execution path doesn't and shouldn't.
//
// `rules` is unused (recomputed inside `ApplyUpgrades`) but retained for
// interface symmetry.
func (h *hooks) StartExecutingBlock(_ ethparams.Rules, statedb *state.StateDB, parent *types.Header, block *types.Block) error {
	blockContext := legacycore.NewBlockContext(block.Number(), block.Time())
	if err := legacycore.ApplyUpgrades(h.chainConfig, &parent.Time, blockContext, statedb); err != nil {
		return fmt.Errorf("applying upgrades for block %s (%d): %w", block.Hash(), block.NumberU64(), err)
	}
	return nil
}

// FinishExecutingBlock is a no-op: the L1 VM has no end-of-block state
// changes outside of the EVM transactions themselves.
func (*hooks) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

// AfterExecutingBlock stores the block's accepted warp messages, keyed for
// later ACP-118 signature requests. It runs only during canonical execution,
// which is exactly the once-per-block semantics warp storage requires.
func (h *hooks) AfterExecutingBlock(b *types.Block, receipts types.Receipts) error {
	if mde := customtypes.GetHeaderExtra(b.Header()).MinDelayExcess; mde != nil {
		h.metrics.SetMinBlockDelay(dynamic.DelayExponent(*mde).DelayDuration())
	}

	// Genesis and state upgrades can place arbitrary code at the Warp address.
	// Only once the precompile is active are its logs guaranteed to be Warp
	// messages.
	rules := h.chainConfig.Rules(b.Number(), legacyparams.IsMergeTODO, b.Time())
	if _, ok := activeWarpConfig(rules); !ok {
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

// activeWarpConfig returns the Warp precompile config in `rules`, or false if
// Warp is not active.
func activeWarpConfig(rules ethparams.Rules) (*legacywarp.Config, bool) {
	config, ok := legacyparams.GetRulesExtra(rules).Precompiles[legacywarp.ContractAddress].(*legacywarp.Config)
	return config, ok
}

var _ hookadaptor.BlockBuilder = (*builder)(nil)

type builder struct {
	ctx         *snow.Context
	chainConfig *ethparams.ChainConfig

	now func() time.Time
	// When fields in desired are set, the block builder will build blocks
	// that move the network values towards their desired values.
	desired saeconfig.Desired

	// coinbase is the fee recipient stamped into `header.Coinbase`
	// in operator-chosen branches of [builder.resolveCoinbase].
	// On a builder, it is the local node's configured fee recipient.
	// On a rebuilder it is overridden with the RECEIVED block's
	// Coinbase so the rebuilt header hashes identically to the received
	// header. See [hooks.BlockRebuilderFrom] for the determinism
	// rationale.
	coinbase common.Address
	// building is true iff the builder is building original blocks.
	building bool
}

var (
	errIglooUnactivated   = errors.New("igloo is not activated")
	errBelowMinBlockDelay = errors.New("block time below the ACP-226 minimum block delay")
)

// earliestBlockTime returns the earliest wall-clock time at which a child of
// `parent` may be built. When `parent` carries no `MinDelayExcess` the
// ACP-226 minimum-delay rule does not apply (matching the consensus check in
// `customheader.VerifyTime`), so there is no minimum; block-time monotonicity
// is enforced by the SAE core regardless.
func earliestBlockTime(parent *types.Header) time.Time {
	e := customtypes.GetHeaderExtra(parent)
	if e.MinDelayExcess == nil {
		return time.Time{}
	}
	return blockTime(parent).Add(dynamic.DelayExponent(*e.MinDelayExcess).DelayDuration())
}

// See [hook.BlockBuilder.BuildHeader] for which fields MUST or MAY be set in
// the returned header.
func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	now := b.now()
	configExtra := legacyparams.GetExtra(b.chainConfig)
	nowUnix := uint64(now.Unix()) //#nosec G115 -- block times are known non-negative
	if !configExtra.IsIgloo(nowUnix) {
		return nil, errIglooUnactivated
	}
	if earliest := earliestBlockTime(parent); now.Before(earliest) {
		return nil, fmt.Errorf("%w: block time %s is before the minimum %s",
			errBelowMinBlockDelay, now, earliest)
	}
	nowMS := uint64(now.UnixMilli()) //#nosec G115 -- block times are known non-negative

	mde := dynamic.InitialDelayExponent
	if pmde := customtypes.GetHeaderExtra(parent).MinDelayExcess; pmde != nil {
		mde = dynamic.DelayExponent(*pmde)
	}
	minDelayExcess := acp226.DelayExcess(mde.Toward(b.desired.DelayExponent))

	te := gas.Gas(dynamic.TargetExponent(targetExcess(parent)).Toward(b.desired.TargetExponent))
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: parent.Hash(),
			// `Coinbase` is a placeholder; the final value may depend on
			// settled-state-as-of-build-time (rewardmanager precompile)
			// which is not in scope here. [builder.BuildBlock]
			// receives the worst-case state from SAE and overwrites this field
			// before the block is sealed (see [builder.resolveCoinbase]).
			Coinbase:         blackhole.Address,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(now.Unix()), //#nosec G115 -- block times are known non-negative
			BlobGasUsed:      new(uint64(0)),
			ExcessBlobGas:    new(uint64(0)),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			// BlockGasCost is preserved in the header for layout parity with
			// legacy headers, but is not consumed for any
			// decision-making in SAE (ACP-226 superseded its use). It is
			// always stamped to zero by the SAE block builder.
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: new(uint64(nowMS)),
			MinDelayExcess:   &minDelayExcess,
			TargetExcess:     &te,
		},
	), nil
}

var (
	errZeroStoredGasPriceConfig = errors.New("gaspricemanager enabled but storage is zero")
	errEmptyBlock               = errors.New("empty block")
)

// BuildBlock also stamps the header fields that depend on the settled block:
// the effective ACP-224 gas-config group (read from gaspricemanager storage
// in `worstCaseState`; see [gasConfigFromStored]) and `header.Coinbase` (see
// [builder.resolveCoinbase]). Both gates use `settledRules` (NOT the rules at
// `header.Time`): a precompile activation `T` is only reflected in
// `worstCaseState` storage once a block with `T <= blockTime` has settled;
// using header.Time would read an uninitialised slot.
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

	if legacyparams.GetRulesExtra(settledRules).IsPrecompileEnabled(gaspricemanager.ContractAddress) {
		stored := gaspricemanager.GetStoredGasPriceConfig(worstCaseState, gaspricemanager.ContractAddress)
		if stored == (commontype.GasPriceConfig{}) {
			// Activation runs [gaspricemanager.Configure] which always writes
			// a non-zero config (MinGasPrice > 0), and the only mutator path
			// also enforces non-zero via [commontype.GasPriceConfig.Verify].
			// Reaching here therefore indicates corrupt or missing storage at
			// an activated precompile, which would cause silent divergence if
			// papered over with defaults.
			return nil, fmt.Errorf("%w: block %d settling %d", errZeroStoredGasPriceConfig, header.Number, settled.Height)
		}
		headerExtra := customtypes.GetHeaderExtra(header)
		if !stored.ValidatorTargetGas {
			targetExcess := gas.Gas(dynamic.DesiredTargetExponent(gas.Gas(stored.TargetGas)))
			headerExtra.TargetExcess = &targetExcess
		}
		stampGasConfig(headerExtra, gasConfigFromStored(stored))
	}
	header.Coinbase = b.resolveCoinbase(settledRules, worstCaseState)

	rules := b.chainConfig.Rules(header.Number, legacyparams.IsMergeTODO, header.Time)
	var warpValidity predicate.BlockResults
	if warpConfig, ok := activeWarpConfig(rules); ok {
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

	// Encode the settled marker into the header so [hooks.SettledBy] can
	// recover it.
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

// resolveCoinbase returns the fee recipient for the block being built.
// Branches, in order:
//  1. rewardmanager precompile not enabled under `settledRules`:
//     the configured coinbase if `AllowFeeRecipients` is true, else burn.
//  2. precompile enabled, allows fee recipients enabled:
//     the configured coinbase.
//  3. otherwise: the address stored in the precompile's reward address slot.
//
// Gates use `settledRules` (not the rules at `header.Time`): see
// [builder.BuildBlock].
func (b *builder) resolveCoinbase(settledRules ethparams.Rules, worstCaseState libevm.StateReader) common.Address {
	if !legacyparams.GetRulesExtra(settledRules).IsPrecompileEnabled(rewardmanager.ContractAddress) {
		configExtra := legacyparams.GetExtra(b.chainConfig)
		if configExtra.AllowFeeRecipients {
			return b.customCoinbase()
		}
		return blackhole.Address
	}

	addr, allowFeeRecipients := rewardmanager.GetStoredRewardAddress(worstCaseState)
	if allowFeeRecipients {
		return b.customCoinbase()
	}
	return addr
}

// customCoinbase returns [builder.coinbase], warning if it burns the fees of a
// block built by this node.
func (b *builder) customCoinbase() common.Address {
	if b.building && b.coinbase == blackhole.Address {
		b.ctx.Log.Warn(`burning priority fees; set "fee-recipient" to claim them`)
	}
	return b.coinbase
}
