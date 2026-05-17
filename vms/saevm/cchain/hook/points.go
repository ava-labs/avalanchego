// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"
	"iter"
	"slices"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/txpool"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/x/blockdb"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	saestate "github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ hook.PointsG[*cchainTx] = (*Points)(nil)

type Points struct {
	blockBuilder
	state       *saestate.State
	warpStorage *warp.Storage
}

func NewPoints(
	ctx *snow.Context,
	state *saestate.State,
	chainConfig *ethparams.ChainConfig,
	desiredDelayExcess *acp226.DelayExcess,
	desiredTargetExcess *acp176.TargetExcess,
	pool *txpool.Pending,
	warpStorage *warp.Storage,
) *Points {
	potentialTxs := func() iter.Seq[*cchainTx] {
		return func(yield func(*cchainTx) bool) {
			for rawTx := range pool.Iter() {
				wrapped, err := newCChainTx(rawTx, ctx.AVAXAssetID)
				if err != nil {
					ctx.Log.Warn("failed to wrap pool tx",
						zap.Stringer("txID", rawTx.ID()),
						zap.Error(err),
					)
					continue
				}
				if !yield(wrapped) {
					return
				}
			}
		}
	}
	return &Points{
		blockBuilder{
			ctx: ctx,
			desired: params{
				delayExcess:  desiredDelayExcess,
				targetExcess: desiredTargetExcess,
			},
			potentialTxs: potentialTxs,
			chainConfig:  chainConfig,
		},
		state,
		warpStorage,
	}
}

func (p *Points) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*cchainTx], error) {
	rawTxs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	txs := make([]*cchainTx, len(rawTxs))
	for i, rawTx := range rawTxs {
		tx, err := newCChainTx(rawTx, p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		txs[i] = tx
	}

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
		potentialTxs: func() iter.Seq[*cchainTx] {
			return slices.Values(txs)
		},
	}, nil
}

func (p *Points) ExecutionResultsDB(dataDir string) (saetypes.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		p.ctx.Log,
	)
	return saetypes.ExecutionResults{HeightIndex: db}, err
}

// ExecutionArtifact returns nil: C-chain has no precompile-driven gas
// configuration, so nothing needs to be persisted alongside execution results.
func (*Points) ExecutionArtifact(*types.Header, libevm.StateReader) ([]byte, error) {
	return nil, nil
}

// GasConfigAt returns ACP-176 defaults derived purely from `h`.
func (p *Points) GasConfigAt(h *types.Header, _ libevm.StateReader) (gas.Gas, gastime.GasPriceConfig, error) {
	return p.GasConfigAfter(h)
}

// GasConfigAfter returns ACP-176 defaults derived purely from `h`. C-chain
// has no precompile-driven fee configuration, so no artifact lookup is
// performed.
func (*Points) GasConfigAfter(h *types.Header) (gas.Gas, gastime.GasPriceConfig, error) {
	return targetExcess(h).Target(), gastime.GasPriceConfig{
		TargetToExcessScaling: acp176.TargetToExcessScaling,
		MinPrice:              acp176.MinPrice,
	}, nil
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

func (p *Points) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := tx.AsOp(p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d to op for block %s (%d): %w", i, b.Hash(), b.NumberU64(), err)
		}
		ops[i] = op
	}
	return ops, nil
}

// CanExecuteTransaction is a no-op for coreth: the C-Chain has no
// txallowlist-style admission precompile, so worst-case admission has nothing
// to enforce. The signature is satisfied to match [hook.Points].
func (*Points) CanExecuteTransaction(ethparams.Rules, common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*Points) RequiresTransactionAdmissionCheck(ethparams.Rules) bool {
	return false
}

func (*Points) BeforeExecutingBlock(ethparams.Rules, *types.Header, *state.StateDB, *types.Block) error {
	return nil
}

func (p *Points) AfterExecutingBlock(statedb *state.StateDB, b *types.Block, receipts types.Receipts) error {
	rules := p.chainConfig.Rules(b.Number(), corethparams.IsMergeTODO, b.Time())
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: p.ctx,
		Warp:    p.warpStorage,
	}
	if err := warp.HandlePrecompileAccept(rules, acceptCtx, receipts); err != nil {
		return fmt.Errorf("failed to handle precompile accept for block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return fmt.Errorf("failed to extract txs of block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}

	extstatedb := extstate.New(statedb)
	for _, tx := range txs {
		if err := tx.TransferNonAVAX(p.ctx.AVAXAssetID, extstatedb); err != nil {
			return fmt.Errorf("failed to transfer non-AVAX assets of tx %s in block %s (%d): %w", tx.ID(), b.Hash(), b.NumberU64(), err)
		}
	}

	height := b.NumberU64()
	if err := p.state.Apply(height, txs); err != nil {
		return fmt.Errorf("failed to apply state for block %s (%d): %w", b.Hash(), height, err)
	}
	return nil
}
