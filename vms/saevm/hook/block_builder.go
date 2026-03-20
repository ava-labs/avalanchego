// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/hook"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"
)

var _ hook.BlockBuilder[*txpool.Tx] = (*blockBuilder)(nil)

type blockBuilder struct {
	ctx *snow.Context

	now          func() time.Time
	potentialTxs func() iter.Seq[*txpool.Tx]
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

func (b *blockBuilder) PotentialEndOfBlockOps() iter.Seq[*txpool.Tx] {
	var (
		header      *types.Header
		settledHash common.Hash
		getBlock    blocks.EthBlockSource
	)

	return func(yield func(*txpool.Tx) bool) {
		consumedUTXOs, err := ancestorUTXOIDs(header, settledHash, getBlock)
		if err != nil {
			b.ctx.Log.Error("failed to get ancestor UTXO IDs",
				zap.Error(err),
			)
			return
		}

		for tx := range b.potentialTxs() {
			if consumedUTXOs.Overlaps(tx.Inputs) {
				b.ctx.Log.Debug("tx consumes previously consumed UTXOs",
					zap.Stringer("txID", tx.ID),
				)
				continue
			}
			if err := tx.Tx.Verify(context.TODO(), b.ctx); err != nil {
				b.ctx.Log.Debug("tx failed verification",
					zap.Stringer("txID", tx.ID),
					zap.Error(err),
				)
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
	poolTxs []*txpool.Tx,
) (*types.Block, error) {
	if len(txs) == 0 && len(poolTxs) == 0 {
		return nil, errEmptyBlock
	}

	atomicTxs := make([]*tx.Tx, len(poolTxs))
	for i, poolTx := range poolTxs {
		atomicTxs[i] = poolTx.Tx
	}
	extData, err := tx.MarshalSlice(atomicTxs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal atomic transactions: %w", err)
	}

	return customtypes.NewBlockWithExtData(
		header,
		txs,
		nil, // uncles
		receipts,
		trie.NewStackTrie(nil),
		extData,
		true, // update [customtypes.HeaderExtra.ExtDataHash]
	), nil
}
