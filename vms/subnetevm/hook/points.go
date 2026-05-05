// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/subnetevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp"
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
	return saetypes.ExecutionResults{HeightIndex: db}, err
}

func (*Points) GasConfigAfter(h *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	return targetExcess(h).Target(), gastime.GasPriceConfig{
		TargetToExcessScaling: acp176.TargetToExcessScaling,
		MinPrice:              acp176.MinPrice,
	}
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
