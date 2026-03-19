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
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[*txpool.Transaction] = (*points)(nil)

type points struct {
	blockBuilder
}

func (h *points) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Transaction], error) {
	return &blockBuilder{
		log: h.log,
		now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
		potentialTxs: txpool.NewTxs(), // TODO: FIXME
	}, nil
}

func (h *points) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		h.log,
	)
	return saedb.ExecutionResults{HeightIndex: db}, err
}

func (*points) GasConfigAfter(*types.Header) (gas.Gas, hook.GasPriceConfig) {
	return 1_000_000, hook.GasPriceConfig{
		TargetToExcessScaling: 87,
		MinPrice:              1,
		StaticPricing:         false,
	}
}

func (*points) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (p *points) EndOfBlockOps(b *types.Block) ([]hook.Op, error) {
	txs, err := tx.ParseSlice(customtypes.BlockExtData(b))
	if err != nil {
		return nil, fmt.Errorf("failed to extract txs of block %s (%d): %v", b.Hash(), b.NumberU64(), err)
	}

	ops := make([]hook.Op, len(txs))
	for i, tx := range txs {
		op, err := tx.AsOp(p.ctx.AVAXAssetID)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tx %d to op for block %s (%d): %v", i, b.Hash(), b.NumberU64(), err)
		}
		ops[i] = op
	}
	return ops, nil
}

func (*points) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*points) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*points) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {
	// TODO: Execute atomic ops here
}
