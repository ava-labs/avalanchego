// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"errors"
	"fmt"
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
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/subnetevm/warp"

	subnetevmcore "github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	subnetevmparams "github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	sharedwarp "github.com/ava-labs/avalanchego/vms/saevm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ saehook.PointsG[*Tx] = (*Points)(nil)

type Points struct {
	blockBuilder
	warpStorage *sharedwarp.Storage
}

// NewPoints constructs a new [Points] for use as a [saehook.PointsG].
func NewPoints(
	ctx *snow.Context,
	chainConfig *ethparams.ChainConfig,
	now func() time.Time,
	desiredDelayExcess *acp226.DelayExcess,
	desiredTargetExcess *acp176.TargetExcess,
	warpStorage *sharedwarp.Storage,
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
	return saehook.NewBlockDBExecutionResults(dataDir, p.ctx.Log)
}

// GasConfigAfter derives the gas target and price config in effect after `h`
// purely from the header (plus, for the genesis block, the chain config):
//
//  1. `h` carries a gas-config group (see [headerGasConfig], stamped by
//     [blockBuilder.FinalizeHeader] whenever gaspricemanager is enabled at
//     the settled timestamp): the group is authoritative.
//     `ValidatorTargetGas=true` keeps the header's `TargetExcess` as the
//     target authority; false pins the target from precompile storage.
//  2. `h` is the genesis block (synchronously executed, so never stamped)
//     and gaspricemanager is enabled at genesis: the group is derived from
//     the chain config exactly as [gaspricemanager.Configure] seeded storage.
//  3. Otherwise ACP-176 defaults apply, with the target from `TargetExcess`.
func (p *Points) GasConfigAfter(h *types.Header) (gas.Gas, gastime.GasPriceConfig) {
	headerTarget := targetExcess(h).Target()
	if cfg, ok := readGasConfig(customtypes.GetHeaderExtra(h)); ok {
		return cfg.effective(headerTarget)
	}
	if h.Number.Sign() == 0 {
		if cfg, ok := p.genesisGasConfig(h.Time); ok {
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
func (p *Points) genesisGasConfig(genesisTime uint64) (headerGasConfig, bool) {
	configExtra := subnetevmparams.GetExtra(p.chainConfig)
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

func targetExcess(h *types.Header) acp176.TargetExcess {
	if te := customtypes.GetHeaderExtra(h).TargetExcess; te != nil {
		return *te
	}
	return 0
}

// SettledBy returns the settlement marker encoded in the header by
// [blockBuilder.BuildBlock], or the zero value (indicating synchronous,
// pre-SAE execution) when any of the quartet is missing.
func (*Points) SettledBy(h *types.Header) saehook.Settled {
	he := customtypes.GetHeaderExtra(h)
	return saehook.NewSettled(he.SettledHeight, he.SettledGasUnix, he.SettledGasNumerator, he.SettledExcess)
}

func (*Points) BlockTime(h *types.Header) time.Time {
	return saehook.BlockTimeFrom(h.Time, customtypes.GetHeaderExtra(h).TimeMilliseconds)
}

var (
	// errNonZeroBlockGasCost is returned by [Points.VerifyBlockSyntax] for a
	// header whose BlockGasCost is neither nil nor zero: SAE always stamps
	// zero (ACP-226 superseded its use).
	errNonZeroBlockGasCost = errors.New("non-zero BlockGasCost under SAE")
	// errPartialSettledMarker is returned by [Points.VerifyBlockSyntax] when
	// only some of the Settled* header fields are set; [Points.SettledBy]
	// requires all-or-nothing.
	errPartialSettledMarker = errors.New("partially populated settled marker")
	// errPartialGasConfig is returned by [Points.VerifyBlockSyntax] when only
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
func (*Points) VerifyBlockSyntax(b *types.Block) error {
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
	for _, f := range []*uint64{
		he.GasConfigValidatorTargetGas,
		he.GasConfigTargetGas,
		he.GasConfigTargetToExcessScaling,
		he.GasConfigMinGasPrice,
		he.GasConfigStaticPricing,
	} {
		if f != nil {
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
// EVM execution rather than emitting deferred ops. See [Tx] for details.
func (*Points) EndOfBlockOps(*types.Block) ([]saehook.Op, error) {
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
func (*Points) CanExecuteTransaction(rules ethparams.Rules, from common.Address, _ *common.Address, state libevm.StateReader) error {
	extra := subnetevmparams.GetRulesExtra(rules)
	return subnetevmparams.RulesExtra(*extra).EnforceTxAllowList(from, state)
}

func (*Points) RequiresTransactionAdmissionCheck(rules ethparams.Rules) bool {
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
// [blockBuilder.FinalizeHeader] and [Points.CanExecuteTransaction]:
// `ApplyUpgrades` requires a contiguous (parentTimestamp, blockTimestamp]
// activation window, and the live `statedb` must carry parent's full
// post-exec mutations into the upcoming `core.ApplyTransaction` loop. The
// build/admit-time worst-case path tolerates the Tau lag; this post-Tau
// execution path doesn't and shouldn't.
//
// `rules` is unused (recomputed inside `ApplyUpgrades`) but retained for
// interface symmetry.
func (p *Points) StartExecutingBlock(_ ethparams.Rules, statedb *state.StateDB, parent *types.Header, block *types.Block) error {
	blockContext := subnetevmcore.NewBlockContext(block.Number(), block.Time())
	if err := subnetevmcore.ApplyUpgrades(p.chainConfig, &parent.Time, blockContext, statedb); err != nil {
		return fmt.Errorf("applying upgrades for block %s (%d): %w", block.Hash(), block.NumberU64(), err)
	}
	return nil
}

// FinishExecutingBlock is a no-op: subnet-evm has no end-of-block state
// changes outside of the EVM transactions themselves.
func (*Points) FinishExecutingBlock(*state.StateDB, *types.Block, types.Receipts) error {
	return nil
}

// AfterExecutingBlock stores the block's accepted warp messages, keyed for
// later ACP-118 signature requests. It runs only during canonical execution,
// which is exactly the once-per-block semantics warp storage requires.
func (p *Points) AfterExecutingBlock(b *types.Block, receipts types.Receipts) error {
	rules := p.chainConfig.Rules(b.Number(), subnetevmparams.IsMergeTODO, b.Time())
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: p.ctx,
		Warp:    p.warpStorage,
	}
	if err := warp.HandlePrecompileAccept(rules, acceptCtx, receipts); err != nil {
		return fmt.Errorf("handling precompile accept for block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}
	return nil
}
