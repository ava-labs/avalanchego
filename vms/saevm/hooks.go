// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"errors"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/hook"
	"github.com/ava-labs/strevm/saedb"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[*txpool.Transaction] = (*hooks)(nil)

type hooks struct {
	blockBuilder
	log logging.Logger
}

func (h *hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[*txpool.Transaction], error) {
	return &blockBuilder{
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

func (*hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {}

var _ hook.BlockBuilder[*txpool.Transaction] = (*blockBuilder)(nil)

type blockBuilder struct {
	now          func() time.Time
	potentialTxs *txpool.Txs
}

func (b *blockBuilder) BuildHeader(parent *types.Header) *types.Header {
	var now time.Time
	if b.now != nil {
		now = b.now()
	} else {
		now = time.Now()
	}
	return &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       uint64(now.Unix()),
	}
}

func (b *blockBuilder) PotentialEndOfBlockOps() iter.Seq[*txpool.Transaction] {
	return b.potentialTxs.Iter()
}

var errEmptyBlock = errors.New("empty block")

func (*blockBuilder) BuildBlock(
	header *types.Header,
	txs []*types.Transaction,
	receipts []*types.Receipt,
	atomicTxs []*txpool.Transaction,
) (*types.Block, error) {
	if len(txs) == 0 && len(atomicTxs) == 0 {
		return nil, errEmptyBlock
	}
	// TODO: Include atomic txs
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil)), nil
}
