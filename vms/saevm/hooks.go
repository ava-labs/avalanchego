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
	"github.com/ava-labs/avalanchego/x/blockdb"
)

var _ hook.PointsG[hook.Transaction] = (*Hooks)(nil)

type Hooks struct {
	Now func() time.Time
}

func (h *Hooks) BuildHeader(parent *types.Header) *types.Header {
	var now time.Time
	if h.Now != nil {
		now = h.Now()
	} else {
		now = time.Now()
	}
	return &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       uint64(now.Unix()),
	}
}

func emptyIter[T any](func(T) bool) {}

func (*Hooks) PotentialEndOfBlockOps() iter.Seq[hook.Transaction] {
	return emptyIter[hook.Transaction]
}

var errEmptyBlock = errors.New("empty block")

func (*Hooks) BuildBlock(header *types.Header, txs []*types.Transaction, receipts []*types.Receipt, _ []hook.Transaction) (*types.Block, error) {
	if len(txs) == 0 {
		return nil, errEmptyBlock
	}
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil)), nil
}

func (*Hooks) BlockRebuilderFrom(b *types.Block) (hook.BlockBuilder[hook.Transaction], error) {
	return &Hooks{
		Now: func() time.Time {
			return time.Unix(int64(b.Time()), 0)
		},
	}, nil
}

func (*Hooks) ExecutionResultsDB(dataDir string) (saedb.ExecutionResults, error) {
	db, err := blockdb.New(
		blockdb.DefaultConfig().WithDir(dataDir),
		logging.NoLog{},
	)
	return saedb.ExecutionResults{HeightIndex: db}, err
}

func (*Hooks) GasTargetAfter(*types.Header) gas.Gas {
	return 1_000_000
}

func (*Hooks) SubSecondBlockTime(*types.Header) time.Duration {
	return 0
}

func (*Hooks) EndOfBlockOps(*types.Block) ([]hook.Op, error) {
	return nil, nil
}

func (*Hooks) CanExecuteTransaction(common.Address, *common.Address, libevm.StateReader) error {
	return nil
}

func (*Hooks) BeforeExecutingBlock(params.Rules, *state.StateDB, *types.Block) error {
	return nil
}

func (*Hooks) AfterExecutingBlock(*state.StateDB, *types.Block, types.Receipts) {}
