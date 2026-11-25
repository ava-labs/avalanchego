// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"errors"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/worstcase"
)

// DefaultExternalBuilderTimeout is used when [Config.ExternalBuilderTimeout]
// is not positive.
const DefaultExternalBuilderTimeout = 200 * time.Millisecond

var errBlockByteBudget = errors.New("block byte budget exceeded")

// An ExternalBuilder proposes the leading transactions of blocks built by this
// node, typically by delegating to a block-building process running outside of
// the node.
//
// A proposal can only choose which valid transactions a block starts with, and
// their order. Proposed transactions are subject to the same worst-case
// validation as those from the mempool, so an ExternalBuilder can not cause the
// node to build an invalid block. Proposals are all-or-nothing: if any proposed
// transaction can not be included, or the proposal is not returned in time,
// the node builds the block from its mempool alone.
//
// Only blocks built by this node consult the ExternalBuilder. Verification of
// blocks, including those built by this node, is unaffected.
type ExternalBuilder interface {
	// ProposeTxs returns the transactions to place, in order, at the start of
	// the block being built with the provided header. The header's
	// [types.Header.GasLimit] and [types.Header.BaseFee] are the worst-case
	// values against which the transactions will be validated, and its
	// [types.Header.Root] is the post-execution state root of the last block
	// that it settles. The header MAY be modified.
	ProposeTxs(ctx context.Context, header *types.Header) ([]*types.Transaction, error)
}

// proposedTxs applies the transactions proposed by external, which MAY be nil,
// to state, which MUST have started the block with header hdr. It returns them
// and their total size.
//
// If any of them can not be included, none are: state is left unchanged and
// nil is returned.
func (b *blockBuilderG[_]) proposedTxs(
	ctx context.Context,
	external ExternalBuilder,
	hdr *types.Header,
	state *worstcase.State,
	byteBudget uint64,
	log logging.Logger,
) ([]*types.Transaction, uint64) {
	if external == nil {
		return nil, 0
	}
	timeout := b.externalTimeout
	if timeout <= 0 {
		timeout = DefaultExternalBuilderTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	txs, err := external.ProposeTxs(ctx, types.CopyHeader(hdr))
	if err != nil {
		log.Warn("External builder failed to propose transactions",
			zap.Error(err),
		)
		return nil, 0
	}

	snap := state.Snapshot()
	var size uint64
	for i, tx := range txs {
		size += tx.Size()
		err := errBlockByteBudget
		if size <= byteBudget {
			err = state.ApplyTx(tx)
		}
		if err != nil {
			state.Revert(snap)
			log.Warn("Building block without external proposal",
				zap.Int("tx_index", i),
				zap.Stringer("tx_hash", tx.Hash()),
				zap.Error(err),
			)
			return nil, 0
		}
	}
	return txs, size
}
