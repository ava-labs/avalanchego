// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"errors"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
)

var _ hook.BlockBuilder[*txpool.Transaction] = (*blockBuilder)(nil)

type blockBuilder struct {
	log logging.Logger

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

func emptyIter(yield func(*txpool.Transaction) bool) {}

func (b *blockBuilder) PotentialEndOfBlockOps() iter.Seq[*txpool.Transaction] {
	var (
		header      *types.Header
		settledHash common.Hash
		getBlock    blocks.EthBlockSource
	)
	consumedUTXOs, err := ancestorUTXOIDs(header, settledHash, getBlock)
	if err != nil {
		b.log.Error("failed to get ancestor UTXO IDs",
			zap.Error(err),
		)
		return emptyIter
	}

	return func(yield func(*txpool.Transaction) bool) {
		for tx := range b.potentialTxs.Iter() {
			if consumedUTXOs.Overlaps(tx.Inputs) {
				continue
			}

			if err := tx.Tx.Verify(&snow.Context{}, extras.Rules{}); err != nil {
				continue
			}

			if !yield(tx) {
				return
			}

			consumedUTXOs.Union(tx.Inputs)
		}
	}
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
