// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[*txpool.Transaction] = (*hooks)(nil)

type hooks struct {
	blockBuilder
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Transaction], error) {
	return &blockBuilder{
		log: h.log,
		now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
		potentialTxs: txpool.NewTxs(), // TODO: FIXME
	}, nil
}

func (h *hooks) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		h.log,
	)
	return saedb.ExecutionResults{HeightIndex: db}, err
}

func (*hooks) GasConfigAfter(*types.Header) (gas.Gas, hook.GasPriceConfig) {
	return 1_000_000, hook.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
		StaticPricing:         false,
	}
}

func (*hooks) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (*hooks) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

func (*hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {
	//

	// acceptCtx := &precompileconfig.AcceptContext{
	// 	SnowCtx: b.vm.ctx,
	// 	Warp:    b.vm.warpBackend,
	// }
	// for _, receipt := range receipts {
	// 	for logIdx, log := range receipt.Logs {
	// 		accepter, ok := rules.AccepterPrecompiles[log.Address]
	// 		if !ok {
	// 			continue
	// 		}
	// 		if err := accepter.Accept(acceptCtx, log.BlockHash, log.BlockNumber, log.TxHash, logIdx, log.Topics, log.Data); err != nil {
	// 			return err
	// 		}
	// 	}
	// }
}
