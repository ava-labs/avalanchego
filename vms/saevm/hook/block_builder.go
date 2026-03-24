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
	"github.com/ava-labs/strevm/hook"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/evm/constants"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/acp176"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/txpool"

	saetypes "github.com/ava-labs/strevm/types"
)

var _ hook.BlockBuilder[*txpool.Tx] = (*blockBuilder)(nil)

type blockBuilder struct {
	ctx            *snow.Context
	consensusState *utils.Atomic[snow.State]

	now func() time.Time
	// If the gas target is specified, calculate the desired target excess and
	// use it during block creation.
	desiredTargetExcess *acp176.TargetExcess
	potentialTxs        func() iter.Seq[*txpool.Tx]
}

func (b *blockBuilder) BuildHeader(parent *types.Header) *types.Header {
	var now time.Time
	if b.now != nil {
		now = b.now()
	} else {
		now = time.Now()
	}

	te := targetExcess(parent)
	if b.desiredTargetExcess != nil {
		te.UpdateTargetExcess(*b.desiredTargetExcess)
	}
	return customtypes.WithHeaderExtra(
		&types.Header{
			ParentHash:       parent.Hash(),
			Coinbase:         constants.BlackholeAddr,
			Difficulty:       big.NewInt(1),
			Number:           new(big.Int).Add(parent.Number, common.Big1),
			Time:             uint64(now.Unix()),
			Extra:            nil, // TODO: Include warp predicates
			BlobGasUsed:      utils.PointerTo[uint64](0),
			ExcessBlobGas:    utils.PointerTo[uint64](0),
			ParentBeaconRoot: &common.Hash{},
		},
		&customtypes.HeaderExtra{
			ExtDataGasUsed:   big.NewInt(0),
			BlockGasCost:     big.NewInt(0),
			TimeMilliseconds: utils.PointerTo[uint64](uint64(now.UnixMilli())),
			MinDelayExcess:   utils.PointerTo[acp226.DelayExcess](0), // TODO:
			TargetExcess:     &te,
		},
	)
}

func (b *blockBuilder) PotentialEndOfBlockOps(header *types.Header, settledHash common.Hash, source saetypes.BlockSource) iter.Seq[*txpool.Tx] {
	// During bootstrapping, we may be processing transactions that were
	// previously valid, but are no longer valid. Additionally, Input UTXOs may
	// not have been populated by the source chain. Therefore we skip
	// verification during bootstrapping.
	if b.consensusState.Get() != snow.NormalOp {
		return b.potentialTxs()
	}

	return func(yield func(*txpool.Tx) bool) {
		// Transactions are verified against the last executed state. We must
		// guarantee that they don't conflict with any transactions in blocks
		// between the block we are building and the last executed block.
		consumedUTXOs, err := ancestorUTXOIDs(header, settledHash, source)
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

	// TODO(StephenButtolph): Should only update the ExtDataHash after AP1.
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
