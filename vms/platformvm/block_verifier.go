// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/window"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateless"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	p_block "github.com/ava-labs/avalanchego/vms/platformvm/blocks/stateful"
)

var _ p_block.Verifier = &blkVerifier{}

func NewBlockVerifier(
	mempool mempool.Mempool,
	internalState InternalState,
	txExecutorBackend executor.Backend,
	blkMetrics stateless.Metrics,
	windows *window.Window,
) p_block.Verifier {
	return &blkVerifier{
		Mempool:          mempool,
		InternalState:    internalState,
		Backend:          txExecutorBackend,
		Metrics:          blkMetrics,
		currentBlksCache: make(map[ids.ID]p_block.Block),
		recentlyAccepted: windows,
	}
}

type blkVerifier struct {
	mempool.Mempool
	InternalState
	executor.Backend
	stateless.Metrics

	// Key: block ID
	// Value: the block
	currentBlksCache map[ids.ID]p_block.Block

	recentlyAccepted *window.Window
}

func (bv *blkVerifier) SetHeight(height uint64) { bv.InternalState.SetHeight(height) }

func (bv *blkVerifier) GetStatefulBlock(blkID ids.ID) (p_block.Block, error) {
	// If block is in memory, return it.
	if blk, exists := bv.currentBlksCache[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := bv.InternalState.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return p_block.MakeStateful(statelessBlk, bv, bv.Backend, blkStatus)
}

func (bv *blkVerifier) StateContentForApply() state.State { return bv.InternalState }
func (bv *blkVerifier) GetMutableState() state.Chain      { return bv.InternalState }

func (bv *blkVerifier) CacheVerifiedBlock(blk p_block.Block) {
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
