// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

type Verifier interface {
	mempool.Mempool
	state.State
	stateless.Metrics

	GetStatefulBlock(blkID ids.ID) (Block, error)
	CacheVerifiedBlock(Block)
	DropVerifiedBlock(blkID ids.ID)

	// register recently accepted blocks, needed
	// to calculate the minimum height of the block still in the
	// Snowman++ proposal window.
	AddToRecentlyAcceptedWindows(blkID ids.ID)
}

func NewBlockVerifier(
	mempool mempool.Mempool,
	state state.State,
	txExecutorBackend executor.Backend,
	blkMetrics stateless.Metrics,
	windows *window.Window,
) Verifier {
	return &blkVerifier{
		Mempool:          mempool,
		State:            state,
		Backend:          txExecutorBackend,
		Metrics:          blkMetrics,
		currentBlksCache: make(map[ids.ID]Block),
		recentlyAccepted: windows,
	}
}

type blkVerifier struct {
	mempool.Mempool
	state.State
	executor.Backend
	stateless.Metrics

	// Key: block ID
	// Value: the block
	currentBlksCache map[ids.ID]Block

	recentlyAccepted *window.Window
}

func (bv *blkVerifier) GetStatefulBlock(blkID ids.ID) (Block, error) {
	// If block is in memory, return it.
	if blk, exists := bv.currentBlksCache[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := bv.State.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return MakeStateful(statelessBlk, bv, bv.Backend, blkStatus)
}

func (bv *blkVerifier) CacheVerifiedBlock(blk Block) {
	bv.currentBlksCache[blk.ID()] = blk
}

func (bv *blkVerifier) DropVerifiedBlock(blkID ids.ID) {
	delete(bv.currentBlksCache, blkID)
}

func (bv *blkVerifier) GetRecentlyAcceptedWindows() *window.Window {
	return bv.recentlyAccepted
}

func (bv *blkVerifier) AddToRecentlyAcceptedWindows(blkID ids.ID) {
	bv.recentlyAccepted.Add(blkID)
}
