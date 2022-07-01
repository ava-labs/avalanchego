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
)

type backend struct {
	state state.State
}

func (b *backend) setLastAccepted(blkID ids.ID) {
	// TODO implement
}

func (b *backend) getLastAccepted() ids.ID {
	return b.state.GetLastAccepted()
}

func (b *backend) addStatelessBlock(block stateless.Block, status choices.Status) {
	// TODO implement
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

// TODO
func (b *backend) removeDecisionTxs(txs []*txs.Tx) {}

// TODO
func (b *backend) markDropped(txID ids.ID, err string) {}

// TODO
func (b *backend) removeProposalTx(tx *txs.Tx) {}

// TODO
func (b *backend) cacheVerifiedBlock(blk Block) {}

// TODO
// TODO do we even need this or can we just pass parent ID into getStatefulBlock?
func (b *backend) parent(blk *stateless.CommonBlock) (Block, error) {
	parentBlkID := blk.Parent()
	return b.getStatefulBlock(parentBlkID)
}

func (b *backend) getStatefulBlock(blkID ids.ID) (Block, error) {
	return nil, errors.New("TODO")
}

// TODO
func (b *backend) children() []Block {
	return nil
}

// TODO
func (b *backend) addChild(parent Block, child Block) {}

// TODO implement
// TODO specify what type block should be
func (b *backend) onAccept(blk Block) state.Chain {
	return nil
}
