// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

type lastAccepteder interface {
	SetLastAccepted(blkID ids.ID)
	GetLastAccepted() ids.ID
}

type statelessBlockState interface {
	AddStatelessBlock(block stateless.Block, status choices.Status)
	GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error)
}

type backend struct {
	mempool mempool.Mempool
	lastAccepteder
	statelessBlockState
	txExecutorBackend executor.Backend
	verifiedBlksCache map[ids.ID]Block
}

func (b *backend) markAccepted(blk stateless.Block) error {
	// TODO implement
	return errors.New("TODO")
}

func (b *backend) getState() state.State {
	// TODO
	return nil
}

func (b *backend) abort() {
	// TODO
}

func (b *backend) commitBatch() (database.Batch, error) {
	return nil, errors.New("TODO")
}

func (b *backend) commit() error {
	return errors.New("TODO")
}

func (b *backend) add(*txs.Tx) error {
	return errors.New("TODO")
}

func (b *backend) markAcceptedOptionVote() {
	// TODO
}

func (b *backend) markRejectedOptionVote() {
	// TODO
}

func (b *backend) removeDecisionTxs(txs []*txs.Tx) {
	b.mempool.RemoveDecisionTxs(txs)
}

func (b *backend) markDropped(txID ids.ID, err string) {
	b.mempool.MarkDropped(txID, err)
}

func (b *backend) removeProposalTx(tx *txs.Tx) {
	b.mempool.RemoveProposalTx(tx)
}

func (b *backend) cacheVerifiedBlock(blk Block) {
	b.verifiedBlksCache[blk.ID()] = blk
}

func (b *backend) dropVerifiedBlock(id ids.ID) {
	delete(b.verifiedBlksCache, id)
}

// TODO
// TODO do we even need this or can we just pass parent ID into getStatefulBlock?
func (b *backend) parent(blk *stateless.CommonBlock) (Block, error) {
	parentBlkID := blk.Parent()
	return b.getStatefulBlock(parentBlkID)
}

func (b *backend) getStatefulBlock(blkID ids.ID) (Block, error) {
	/* TODO
	// If block is in memory, return it.
	if blk, exists := b.verifiedBlksCache[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := b.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
		return MakeStateful(statelessBlk, b, b.txExecutorBackend, blkStatus)
	*/
	return nil, errors.New("TODO")
}

// TODO implement
// TODO specify what type block should be
func (b *backend) onAccept(blk Block) state.Chain {
	return nil
}
