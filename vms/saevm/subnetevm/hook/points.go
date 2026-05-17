// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/gaspricemanager"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/warp"
	"github.com/ava-labs/avalanchego/x/blockdb"

	subnetevmcore "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ saehook.PointsG[*Tx] = (*Points)(nil)

type Points struct {
	blockBuilder
	warpStorage *warp.Storage
	// xdb is captured by [Points.ExecutionResultsDB] and used by
	// [Points.GasConfigAfter] to load the previously-persisted hook artifact
	// at the height returned by [Points.SettledHeight].
	xdb saetypes.ExecutionResults
}

// NewPoints constructs a new [Points] for use as a [saehook.PointsG].
func NewPoints(
	ctx *snow.Context,
	chainConfig *ethparams.ChainConfig,
	now func() time.Time,
	desiredDelayExcess *acp226.DelayExcess,
	desiredTargetExcess *acp176.TargetExcess,
	warpStorage *warp.Storage,
	configuredCoinbase common.Address,
) *Points {
	return &Points{
		blockBuilder: blockBuilder{
			ctx: ctx,
			desired: params{
				delayExcess:  desiredDelayExcess,
				targetExcess: desiredTargetExcess,
			},
			chainConfig: chainConfig,
			now:         now,
			coinbase:    configuredCoinbase,
		},
		warpStorage: warpStorage,
	}
}

func (p *Points) BlockRebuilderFrom(b *types.Block) (saehook.BlockBuilder[*Tx], error) {
	header := b.Header()
	headerExtra := customtypes.GetHeaderExtra(header)
	return &blockBuilder{
		ctx:         p.ctx,
		chainConfig: p.chainConfig,
		now: func() time.Time {
			return p.BlockTime(header)
		},
		desired: params{
			delayExcess:  headerExtra.MinDelayExcess,
			targetExcess: headerExtra.TargetExcess,
		},
		coinbase: header.Coinbase, // override with received block's Coinbase
	}, nil
}

func (p *Points) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		p.ctx.Log,
	)
	if err != nil {
		return saetypes.ExecutionResults{}, err
	}
	p.xdb = saetypes.ExecutionResults{HeightIndex: db}
	return p.xdb, nil
}

// ExecutionArtifact projects gaspricemanager storage into opaque bytes for
// SAE to persist alongside its execution artifacts. Returns `nil` (read back
// as defaults) when the precompile is not enabled at `h.Time` or has no
// stored configuration.
func (p *Points) ExecutionArtifact(h *types.Header, state libevm.StateReader) ([]byte, error) {
	configExtra := subnetevmparams.GetExtra(p.chainConfig)
	// Gate on `h.Time` (NOT `settled.Time`): `state` here is `h`'s post-exec
	// state.
	if !configExtra.IsPrecompileEnabled(gaspricemanager.ContractAddress, h.Time) {
		return nil, nil
	}
	stored := gaspricemanager.GetStoredGasPriceConfig(state, gaspricemanager.ContractAddress)
	if stored == (commontype.GasPriceConfig{}) {
		// Activation runs [gaspricemanager.configurator.Configure] which
		// always writes a non-zero config (MinGasPrice > 0), and the only
		// mutator path also enforces non-zero via [commontype.GasPriceConfig.Verify].
		// Reaching here therefore indicates corrupt or missing storage at an
		// activated precompile, which would cause silent divergence if
		// papered over with defaults.
		return nil, fmt.Errorf("gaspricemanager enabled at block %d but storage is zero", h.Number)
	}
	art := gasConfigArtifact{
		ValidatorTargetGas: stored.ValidatorTargetGas,
		TargetGas:          gas.Gas(stored.TargetGas),
		GasPriceConfig: gastime.GasPriceConfig{
			TargetToExcessScaling: scalingFromTimeToDouble(stored.TimeToDouble),
			MinPrice:              gas.Price(stored.MinGasPrice),
			StaticPricing:         stored.StaticPricing,
		},
	}
	return art.MarshalCanoto(), nil
}

// GasConfigAt derives the gas config directly from `h`'s post-execution state.
func (p *Points) GasConfigAt(h *types.Header, state libevm.StateReader) (gas.Gas, gastime.GasPriceConfig, error) {
	bytes, err := p.ExecutionArtifact(h, state)
	if err != nil {
		return 0, gastime.GasPriceConfig{}, err
	}
	return gasConfigFromArtifact(targetExcess(h).Target(), bytes)
}

// GasConfigAfter combines the previously-persisted hook artifact with the
// current header. When validators control the target gas, the header's
// `TargetExcess` remains the source of truth; otherwise the artifact pins
// the target. The artifact is loaded from [Points.ExecutionResultsDB] keyed
// by `SettledHeight(h)`; an empty payload falls back to header-derived
// defaults (e.g. for blocks settled before the gaspricemanager precompile
// activated, and for synchronous blocks whose execution results carry no
// artifact). A missing xdb entry is a chain-halting consensus-critical bug:
// every settled height is guaranteed to have an [executionResults] row
// (written by [Block.MarkSynchronous] / [Block.MarkExecuted]), so absence
// indicates a persistence-layer fault rather than a normal state.
func (p *Points) GasConfigAfter(h *types.Header) (gas.Gas, gastime.GasPriceConfig, error) {
	headerTarget := targetExcess(h).Target()
	bytes, err := blocks.HookArtifact(p.xdb, p.SettledHeight(h))
	if err != nil {
		return 0, gastime.GasPriceConfig{}, fmt.Errorf("loading gas-config artifact: %w", err)
	}
	return gasConfigFromArtifact(headerTarget, bytes)
}

func gasConfigFromArtifact(headerTarget gas.Gas, bytes []byte) (gas.Gas, gastime.GasPriceConfig, error) {
	if len(bytes) == 0 {
		return headerTarget, gastime.DefaultGasPriceConfig(), nil
	}
	art := new(gasConfigArtifact)
	if err := art.UnmarshalCanoto(bytes); err != nil {
		return 0, gastime.GasPriceConfig{}, fmt.Errorf("decoding gas-config artifact: %w", err)
	}
	target, cfg := art.effective(headerTarget)
	return target, cfg, nil
}

// scalingFromTimeToDouble converts ACP-224's `TimeToDouble` (seconds) into the
// K/T ratio used by ACP-176 / gastime: K = T * TimeToDouble / ln(2), so
// TargetToExcessScaling = round(TimeToDouble / ln(2)). The default 60s
// round-trips to the ACP-176 default of 87.
//
// For `StaticPricing` configs `TimeToDouble` is 0 and unused (gastime zeroes
// excess in that branch instead of scaling), but the gastime invariant
// requires `TargetToExcessScaling != 0`, so we return the ACP-176 default.
func scalingFromTimeToDouble(ttd uint64) gas.Gas {
	if ttd == 0 {
		return acp176.TargetToExcessScaling
	}
	return gas.Gas(math.Round(float64(ttd) / math.Ln2))
}

func targetExcess(h *types.Header) acp176.TargetExcess {
	if te := customtypes.GetHeaderExtra(h).TargetExcess; te != nil {
		return *te
	}
	return 0
}

func (*Points) SettledHeight(h *types.Header) uint64 {
	if s := customtypes.GetHeaderExtra(h).SettledHeight; s != nil {
		return *s
	}
	return 0
}

func (*Points) BlockTime(h *types.Header) time.Time {
	var ns int64
	if msp := customtypes.GetHeaderExtra(h).TimeMilliseconds; msp != nil {
		ms := *msp % 1000
		frac := time.Duration(ms) * time.Millisecond //#nosec G115 -- ms is bounded to [0, 1000)
		ns = frac.Nanoseconds()
	}
	return time.Unix(int64(h.Time), ns) //#nosec G115 -- Won't overflow for a few millennia
}

// EndOfBlockOps returns the operations to apply at the end of block execution
// outside of the normal EVM transactions.
//
// Subnet-EVM has none: there are no atomic txs, and stateful precompiles
// (nativeminter, rewardmanager, ...) mutate the active StateDB inline during
// EVM execution rather than emitting deferred ops. See [Tx] for details.
func (*Points) EndOfBlockOps(*types.Block) ([]saehook.Op, error) {
	return nil, nil
}

// CanExecuteTransaction enforces the txallowlist sender check against the
// caller-supplied `rules`+`state` pair (typically last-settled), bypassing
// the libevm hook which is short-circuited post-Helicon to avoid fatal-halt
// on stale-state divergence (see [subnetevmparams.RulesExtra.CanExecuteTransaction]).
// Libevm extras MUST be registered first
//
// Deployer allowlist is intentionally NOT enforced here: its libevm hook
// ([subnetevmparams.RulesExtra.CanCreateContract]) runs INSIDE the EVM and
// surfaces failures as `vmerr` (frame-local revert) rather than invalidating the block,
// so SAE has no halt risk to guard against.
// It also covers nested CREATE/CREATE2 frames invisible to admission here.
// Trade-off: deploy txs from non-allow-listed senders are mined with
// status=failed instead of being rejected at ingress.
func (*Points) CanExecuteTransaction(rules ethparams.Rules, from common.Address, _ *common.Address, state libevm.StateReader) error {
	extra := subnetevmparams.GetRulesExtra(rules)
	return subnetevmparams.RulesExtra(*extra).EnforceTxAllowList(from, state)
}

func (*Points) RequiresTransactionAdmissionCheck(rules ethparams.Rules) bool {
	extra := subnetevmparams.GetRulesExtra(rules)
	return extra.IsPrecompileEnabled(txallowlist.ContractAddress)
}

// BeforeExecutingBlock activates / deactivates timestamp-scheduled
// `PrecompileUpgrades` and `StateUpgrades` for the window
// (parent.Time, block.Time()] by delegating to [subnetevmcore.ApplyUpgrades].
//
// SAE's `saexec.Execute` does not call `core.StateProcessor.Process` (it loops
// `core.ApplyTransaction` from libevm directly), so this hook is the single
// place where upgrade activations enter the per-block flow. Mutations made
// here are committed into the block's post-execution state root.
//
// Uses `parent.Time` and `statedb` rooted at `parent.PostExecutionStateRoot()`
// -- NOT the lagged `settled.Time` / lastSettled state used by
// [blockBuilder.resolveCoinbase] and [Points.CanExecuteTransaction]:
// `ApplyUpgrades` requires a contiguous (parentTimestamp, blockTimestamp]
// activation window, and the live `statedb` must carry parent's full
// post-exec mutations into the upcoming `core.ApplyTransaction` loop. The
// build/admit-time worst-case path tolerates the Tau lag; this
// post-Tau execution path doesn't and shouldn't.
//
// `rules` is unused (recomputed inside `ApplyUpgrades`) but retained for
// interface symmetry.
func (p *Points) BeforeExecutingBlock(_ ethparams.Rules, parent *types.Header, statedb *state.StateDB, block *types.Block) error {
	blockContext := subnetevmcore.NewBlockContext(block.Number(), block.Time())
	if err := subnetevmcore.ApplyUpgrades(p.chainConfig, &parent.Time, blockContext, statedb); err != nil {
		return fmt.Errorf("applying upgrades for block %s (%d): %w", block.Hash(), block.NumberU64(), err)
	}
	return nil
}

func (p *Points) AfterExecutingBlock(_ *state.StateDB, b *types.Block, receipts types.Receipts) error {
	rules := p.chainConfig.Rules(b.Number(), subnetevmparams.IsMergeTODO, b.Time())
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: p.ctx,
		Warp:    p.warpStorage,
	}
	if err := warp.HandlePrecompileAccept(rules, acceptCtx, receipts); err != nil {
		return fmt.Errorf("failed to handle precompile accept for block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}
	return nil
}
