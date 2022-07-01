// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var _ blockState = &blockStateImpl{}

type blockState interface {
	AddStatelessBlock(block stateless.Block, status choices.Status)
	GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error)
	GetStatefulBlock(blkID ids.ID) (Block, error)
	pinVerifiedBlock(blk Block)
	unpinVerifiedBlock(id ids.ID)
}

type blockStateImpl struct {
	// TODO is there a way to avoid having [manager] in here?
	// [blockStateImpl] is embedded in manager.
	manager           Manager
	verifiedBlks      map[ids.ID]Block
	txExecutorBackend executor.Backend
}

func (b *blockStateImpl) GetStatefulBlock(blkID ids.ID) (Block, error) {
	// If block is in memory, return it.
	if blk, exists := b.verifiedBlks[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := b.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}

	return MakeStateful(
		statelessBlk,
		b.manager,
		b.txExecutorBackend,
		blkStatus,
	)
}

// TODO
func (b *blockStateImpl) AddStatelessBlock(block stateless.Block, status choices.Status) {}

// TODO
func (b *blockStateImpl) GetStatelessBlock(blockID ids.ID) (stateless.Block, choices.Status, error) {
	return nil, choices.Unknown, errors.New("TODO")
}

func (b *blockStateImpl) pinVerifiedBlock(blk Block) {
	b.verifiedBlks[blk.ID()] = blk
}

func (b *blockStateImpl) unpinVerifiedBlock(id ids.ID) {
	delete(b.verifiedBlks, id)
}
