// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

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
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/rewardmanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"

	subnetevmcore "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	subnetevmwarp "github.com/ava-labs/avalanchego/vms/saevm/subnetevm/warp"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	saewarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ hook.PointsG[*hookTx] = (*hooks)(nil)

type hooks struct {
	builder
	warpStorage *saewarp.Storage
}

func newHooks(
	ctx *snow.Context,
	chainConfig *ethparams.ChainConfig,
	now func() time.Time,
	desired desiredParams,
	warpStorage *saewarp.Storage,
	configuredCoinbase common.Address,
) *hooks {
	return &hooks{
		builder: builder{
			ctx:         ctx,
			chainConfig: chainConfig,
			now:         now,
			desired:     desired,
			coinbase:    configuredCoinbase,
		},
		warpStorage: warpStorage,
	}
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*hookTx], error) {
	header := b.Header()
	headerExtra := customtypes.GetHeaderExtra(header)
	return &builder{
		ctx:         h.ctx,
		chainConfig: h.chainConfig,
		now: func() time.Time {
			return h.BlockTime(header)
		},
		desired: desiredParams{
			delayExcess:  headerExtra.MinDelayExcess,
			targetExcess: headerExtra.TargetExcess,
		},
		coinbase: header.Coinbase, // override with received block's Coinbase
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	return hook.NewBlockDBExecutionResults(dataDir, h.ctx.Log)
}

// GasConfigAfter derives the gas target and price config in effect after `h`
// purely from the header (plus, for the genesis block, the chain config):
//
//  1. `hdr` carries a gas-config group (see [headerGasConfig], stamped by
//     [builder.FinalizeHeader] whenever gaspricemanager is enabled at
//     the settled timestamp): the group is authoritative.
//     `ValidatorTargetGas=true` keeps the header's `TargetExcess` as the
//     target authority; false pins the target from precompile storage.
//  2. `hdr` is the genesis block (synchronously executed, so never stamped)
//     and gaspricemanager is enabled at genesis: the group is derived from
//     the chain config exactly as [gaspricemanager.Configure] seeded storage.
//  3. Otherwise ACP-176 defaults apply, with the target from `TargetExcess`.
func (h *hooks) GasConfigAfter(hdr *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	headerTarget := acp176Target(targetExcess(hdr))
	if cfg, ok := readGasConfig(customtypes.GetHeaderExtra(hdr)); ok {
		return cfg.effective(headerTarget)
	}
	if hdr.Number.Sign() == 0 {
		if cfg, ok := h.genesisGasConfig(hdr.Time); ok {
			return cfg.effective(headerTarget)
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
func (h *hooks) genesisGasConfig(genesisTime uint64) (headerGasConfig, bool) {
	configExtra := subnetevmparams.GetExtra(h.chainConfig)
	if !configExtra.IsPrecompileEnabled(gaspricemanager.ContractAddress, genesisTime) {
		return headerGasConfig{}, false
	}
	precompileConfig := configExtra.GetActivePrecompileConfig(gaspricemanager.ContractAddress, genesisTime)
	stored := commontype.DefaultGasPriceConfig()
	if cfg, ok := precompileConfig.(*gaspricemanager.Config); ok && cfg.InitialGasPriceConfig != nil {
		stored = *cfg.InitialGasPriceConfig
	}
	return gasConfigFromStored(stored), true
}

// targetExcess returns hdr's ACP-176 gas-target vote, defaulting to zero when
// the header does not carry one.
func targetExcess(hdr *types.Header) gas.Gas {
	if te := customtypes.GetHeaderExtra(hdr).TargetExcess; te != nil {
		return *te
	}
	return 0
}

// acp176Target returns the gas target voted for by `excess`.
func acp176Target(excess gas.Gas) gas.Gas {
	s := acp176.State{TargetExcess: excess}
	return s.Target()
}

// SettledBy returns the settlement marker encoded in the header by
// [builder.BuildBlock], or the zero value (indicating synchronous,
// pre-SAE execution) when any of the quartet is missing.
func (*hooks) SettledBy(hdr *types.Header) hook.Settled {
	he := customtypes.GetHeaderExtra(hdr)
	return hook.NewSettled(he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess)
}

func (*hooks) BlockTime(hdr *types.Header) time.Time {
	return hook.BlockTimeFrom(hdr.Time, customtypes.GetHeaderExtra(hdr).TimeMilliseconds)
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

// VerifyBlockSyntax checks the stateless subnet-evm-specific invariants of a
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
		he.GasConfigValidatorTargetGas != nil,
		he.GasConfigTargetGas != nil,
		he.GasConfigTargetToExcessScaling != nil,
		he.GasConfigMinGasPrice != nil,
		he.GasConfigStaticPricing != nil,
	} {
		if set {
			gasConfigFields++
		}
	}
	if gasConfigFields != 0 && gasConfigFields != 5 {
		return fmt.Errorf("%w: %d of 5 fields set", errPartialGasConfig, gasConfigFields)
	}
	return nil
}

// EndOfBlockOps returns the operations to apply at the end of block execution
// outside of the normal EVM transactions.
//
// Subnet-EVM has none: there are no atomic txs, and stateful precompiles
// (nativeminter, rewardmanager, ...) mutate the active StateDB inline during
// EVM execution rather than emitting deferred ops. See [hookTx] for details.
func (*hooks) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

// CanExecuteTransaction enforces the txallowlist sender check against the
// caller-supplied `rules`+`state` pair (worst-case: last-settled; mempool
// admission: last-executed), bypassing the libevm hook which is
// short-circuited post-Helicon to avoid fatal-halt on stale-state divergence
// (see [subnetevmparams.RulesExtra.CanExecuteTransaction]). Libevm extras
// MUST be registered first.
//
// Deployer allowlist is intentionally NOT enforced here: its libevm hook
// ([subnetevmparams.RulesExtra.CanCreateContract]) runs INSIDE the EVM and
// surfaces failures as a frame-local revert rather than invalidating the
// block, so SAE has no halt risk to guard against. It also covers nested
// CREATE/CREATE2 frames invisible to admission here. Trade-off: deploy txs
// from non-allow-listed senders are mined with status=failed instead of
// being rejected at ingress.
func (*hooks) CanExecuteTransaction(rules ethparams.Rules, from common.Address, _ *common.Address, state libevm.StateReader) error {
	extra := subnetevmparams.GetRulesExtra(rules)
	return subnetevmparams.RulesExtra(*extra).EnforceTxAllowList(from, state)
}

func (*hooks) RequiresTransactionAdmissionCheck(rules ethparams.Rules) bool {
	extra := subnetevmparams.GetRulesExtra(rules)
	return extra.IsPrecompileEnabled(txallowlist.ContractAddress)
}

// StartExecutingBlock activates / deactivates timestamp-scheduled
// `PrecompileUpgrades` and `StateUpgrades` for the window
// (parent.Time, block.Time()] by delegating to [subnetevmcore.ApplyUpgrades].
//
// SAE's `saexec.Execute` does not call `core.StateProcessor.Process` (it loops
// `core.ApplyTransaction` from libevm directly), so this hook is the single
// place where upgrade activations enter the per-block flow. Mutations made
// here are committed into the block's post-execution state root.
//
// Uses `parent.Time` and `statedb` rooted at `parent.PostExecutionStateRoot()`
// -- NOT the lagged `settled.Time` / last-settled state used by
// [builder.FinalizeHeader] and [hooks.CanExecuteTransaction]:
// `ApplyUpgrades` requires a contiguous (parentTimestamp, blockTimestamp]
// activation window, and the live `statedb` must carry parent's full
// post-exec mutations into the upcoming `core.ApplyTransaction` loop. The
// build/admit-time worst-case path tolerates the Tau lag; this post-Tau
// execution path doesn't and shouldn't.
//
// `rules` is unused (recomputed inside `ApplyUpgrades`) but retained for
// interface symmetry.
func (h *hooks) StartExecutingBlock(_ ethparams.Rules, statedb *state.StateDB, parent *types.Header, block *types.Block) error {
	blockContext := subnetevmcore.NewBlockContext(block.Number(), block.Time())
	if err := subnetevmcore.ApplyUpgrades(h.chainConfig, &parent.Time, blockContext, statedb); err != nil {
		return fmt.Errorf("applying upgrades for block %s (%d): %w", block.Hash(), block.NumberU64(), err)
	}
	return nil
}

// FinishExecutingBlock is a no-op: subnet-evm has no end-of-block state
// changes outside of the EVM transactions themselves.
func (*hooks) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

// AfterExecutingBlock stores the block's accepted warp messages, keyed for
// later ACP-118 signature requests. It runs only during canonical execution,
// which is exactly the once-per-block semantics warp storage requires.
func (h *hooks) AfterExecutingBlock(b *types.Block, receipts types.Receipts) error {
	rules := h.chainConfig.Rules(b.Number(), subnetevmparams.IsMergeTODO, b.Time())
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: h.ctx,
		Warp:    h.warpStorage,
	}
	if err := subnetevmwarp.HandlePrecompileAccept(rules, acceptCtx, receipts); err != nil {
		return fmt.Errorf("handling precompile accept for block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}
	return nil
}

var _ hook.BlockBuilder[*hookTx] = (*builder)(nil)

// desiredParams bundles this node's votes for the dynamic consensus
// parameters. A nil field means no vote.
type desiredParams struct {
	delayExcess  *acp226.DelayExcess
	targetExcess *gas.Gas
}

type builder struct {
	ctx         *snow.Context
	chainConfig *ethparams.ChainConfig

	now func() time.Time
	// When fields in desired are set, the block builder will build blocks
	// that move the network values towards their desired values.
	desired desiredParams

	// coinbase is the fee recipient stamped into `header.Coinbase`
	// in operator-chosen branches of [builder.resolveCoinbase].
	// On a builder, it is the local node's configured fee recipient.
	// On a rebuilder it is overridden with the RECEIVED block's
	// Coinbase so the rebuilt header hashes identically to the received
	// header. See [hooks.BlockRebuilderFrom] for the determinism
	// rationale.
	coinbase common.Address
}

func (b *builder) BuildHeader(parent *types.Header) (*types.Header, error) {
	now := b.now()
	nowMS := uint64(now.UnixMilli())

	mde := acp226.InitialDelayExcess
	if pmde := customtypes.GetHeaderExtra(parent).MinDelayExcess; pmde != nil {
		mde = *pmde
	}

	{
		parentTimeMS := customtypes.HeaderTimeMilliseconds(parent)
		if nowMS < parentTimeMS {
			return nil, fmt.Errorf("current time is before parent timestamp: now=%d parentTime=%d", nowMS, parentTimeMS)
		}

		delay := nowMS - parentTimeMS
		minDelay := mde.Delay()
		if delay < minDelay {
			return nil, fmt.Errorf("block building separation not satisfied: delay=%d minDelay=%d", delay, minDelay)
		}
	}

	if b.desired.delayExcess != nil {
		mde.UpdateDelayExcess(*b.desired.delayExcess)
	}

	te := targetExcess(parent)
	if b.desired.targetExcess != nil {
		s := acp176.State{TargetExcess: te}
		s.UpdateTargetExcess(*b.desired.targetExcess)
		te = s.TargetExcess
	}
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash: parent.Hash(),
			// `Coinbase` is a placeholder; the final value may depend on
			// settled-state-as-of-build-time (rewardmanager precompile)
			// which is not in scope here. [builder.FinalizeHeader]
			// receives the settled state from SAE and overwrites this field
			// before the block is sealed (see [builder.resolveCoinbase]).
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(now.Unix()),
			BlobGasUsed:      utils.PointerTo[uint64](0),
			ExcessBlobGas:    utils.PointerTo[uint64](0),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			// BlockGasCost is preserved in the header for layout parity with
			// legacy subnet-evm headers, but is not consumed for any
			// decision-making in SAE (ACP-226 superseded its use). It is
			// always stamped to zero by the SAE block builder.
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: utils.PointerTo[uint64](nowMS),
			MinDelayExcess:   &mde,
			TargetExcess:     &te,
		},
	), nil
}

// PotentialEndOfBlockOps returns the iterator of end-of-block transactions to
// consider for inclusion.
//
// Subnet-EVM has no end-of-block ops, so this is always empty. See [hookTx]
// for the rationale.
func (*builder) PotentialEndOfBlockOps(_ context.Context, _ *types.Header, _ common.Hash, _ saetypes.BlockSource) iter.Seq[*hookTx] {
	return func(_ func(*hookTx) bool) {}
}

var errZeroStoredGasPriceConfig = errors.New("gaspricemanager enabled but storage is zero")

// FinalizeHeader stamps the header fields that depend on the settled block:
// the effective ACP-224 gas-config group (read from gaspricemanager storage
// in the settled state; see [headerGasConfig]) and `header.Coinbase` (see
// [builder.resolveCoinbase]). Both gates use `settled.Time` (NOT
// `header.Time`): a precompile activation `T` is only reflected in
// `settledState` once a block with `T <= blockTime` has settled; using
// header.Time would read an uninitialised slot.
//
// The gas-config group MUST be stamped here rather than in
// [builder.BuildBlock] because SAE's worst-case projection reads it off
// the header (via [hooks.GasConfigAfter]) before BuildBlock runs.
func (b *builder) FinalizeHeader(header, settled *types.Header, settledState libevm.StateReader) error {
	configExtra := subnetevmparams.GetExtra(b.chainConfig)
	if configExtra.IsPrecompileEnabled(gaspricemanager.ContractAddress, settled.Time) {
		stored := gaspricemanager.GetStoredGasPriceConfig(settledState, gaspricemanager.ContractAddress)
		if stored == (commontype.GasPriceConfig{}) {
			// Activation runs [gaspricemanager.Configure] which always writes
			// a non-zero config (MinGasPrice > 0), and the only mutator path
			// also enforces non-zero via [commontype.GasPriceConfig.Verify].
			// Reaching here therefore indicates corrupt or missing storage at
			// an activated precompile, which would cause silent divergence if
			// papered over with defaults.
			return fmt.Errorf("%w: block %d settling %d", errZeroStoredGasPriceConfig, header.Number, settled.Number)
		}
		stampGasConfig(customtypes.GetHeaderExtra(header), gasConfigFromStored(stored))
	}

	header.Coinbase = b.resolveCoinbase(settled, settledState)
	return nil
}

var errEmptyBlock = errors.New("empty block")

func (b *builder) BuildBlock(
	header *types.Header,
	blockCtx *block.Context,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	_ []*hookTx,
	settled hook.Settled,
) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}

	rules := b.chainConfig.Rules(header.Number, subnetevmparams.IsMergeTODO, header.Time)
	rulesExtra := subnetevmparams.GetRulesExtra(rules)
	predicateBytes, err := subnetevmwarp.PredicateBytes(b.ctx, blockCtx, rulesExtra, txs)
	if err != nil {
		return nil, fmt.Errorf("generating predicates: %w", err)
	}
	header.Extra = customheader.SetPredicateBytesInExtra(header.Extra, predicateBytes)

	// Encode the settled marker into the header so [hooks.SettledBy] can
	// recover it.
	he := customtypes.GetHeaderExtra(header)
	he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess = settled.AsPointers()

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
//  1. rewardmanager precompile not enabled at `settled.Time`:
//     the configured coinbase if `AllowFeeRecipients` is true, else burn.
//  2. precompile enabled, allows fee recipients enabled:
//     the configured coinbase.
//  3. otherwise: the address stored in the precompile's reward address slot.
//
// Gates use `settled.Time` (not `header.Time`): see
// [builder.FinalizeHeader].
func (b *builder) resolveCoinbase(settled *types.Header, settledState libevm.StateReader) common.Address {
	configExtra := subnetevmparams.GetExtra(b.chainConfig)
	if !configExtra.IsPrecompileEnabled(rewardmanager.ContractAddress, settled.Time) {
		if configExtra.AllowFeeRecipients {
			return b.coinbase
		}
		return constants.BlackholeAddr
	}

	addr, allowFeeRecipients := rewardmanager.GetStoredRewardAddress(settledState)
	if allowFeeRecipients {
		return b.coinbase
	}
	return addr
}

var _ hook.Transaction = (*hookTx)(nil)

// hookTx is the user-defined transaction type carried by SAE end-of-block
// ops.
//
// Subnet-EVM has no end-of-block ops. [hookTx] therefore exists solely as an
// inert placeholder so the [hook.PointsG] generic constraint can be
// satisfied. It is never constructed at runtime and is not expected to gain a
// body.
type hookTx struct{}

func (*hookTx) AsOp() hook.Op { return hook.Op{} }

// Size returns 0: a [hookTx] is never constructed, so it never contributes
// bytes to a block.
func (*hookTx) Size() uint64 { return 0 }
