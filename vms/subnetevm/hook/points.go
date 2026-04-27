// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	saehook "github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/corethvm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp"
	"github.com/ava-labs/avalanchego/x/blockdb"

	corethparams "github.com/ava-labs/avalanchego/graft/coreth/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
	ethparams "github.com/ava-labs/libevm/params"
)

var _ saehook.PointsG[*Tx] = (*Points)(nil)

type Points struct {
	blockBuilder
	db          database.Database
	warpStorage *warp.Storage
}

func NewPoints(
	ctx *snow.Context,
	db database.Database,
	chainConfig *ethparams.ChainConfig,
	desiredDelayExcess *acp226.DelayExcess,
	desiredTargetExcess *acp176.TargetExcess,
	warpStorage *warp.Storage,
) *Points {
	return &Points{
		blockBuilder: blockBuilder{
			ctx: ctx,
			desired: params{
				delayExcess:  desiredDelayExcess,
				targetExcess: desiredTargetExcess,
			},
			chainConfig: chainConfig,
		},
		db:          db,
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
		ms := time.Duration(*msp % 1000)
		frac := ms * time.Millisecond
		ns = frac.Nanoseconds()
	}
	return time.Unix(int64(h.Time), ns)
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

func (*Points) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*Points) BeforeExecutingBlock(ethparams.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (p *Points) AfterExecutingBlock(_ *state.StateDB, b *types.Block, receipts types.Receipts) error {
	rules := p.chainConfig.Rules(b.Number(), corethparams.IsMergeTODO, b.Time())
	acceptCtx := &precompileconfig.AcceptContext{
		SnowCtx: p.ctx,
		Warp:    p.warpStorage,
	}
	if err := warp.HandlePrecompileAccept(rules, acceptCtx, receipts); err != nil {
		return fmt.Errorf("failed to handle precompile accept for block %s (%d): %w", b.Hash(), b.NumberU64(), err)
	}
	return nil
}
