// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

/* TODO remove?
var _ stateful.Verifier = &blkVerifier{}

func NewBlockVerifier(
	mempool mempool.Mempool,
	internalState InternalState,
	txExecutorBackend executor.Backend,
	blkMetrics stateless.Metrics,
	windows *window.Window,
) stateful.Verifier {
	return &blkVerifier{
		Mempool:           mempool,
		InternalState:     internalState,
		Backend:           txExecutorBackend,
		Metrics:           blkMetrics,
		verifiedBlksCache: make(map[ids.ID]stateful.Block),
		recentlyAccepted:  windows,
	}
}

type blkVerifier struct {
	mempool.Mempool
	InternalState
	executor.Backend
	stateless.Metrics

	// verifiedBlksCache holds the blockID -> stateful.Block
	// mapping of all blocks verified but not yet accepted/rejected
	verifiedBlksCache map[ids.ID]stateful.Block

	// recentlyAccepted is a sliding window of blocks
	// that were recently accepted. It's used to support
	// snowman++ protocol
	recentlyAccepted *window.Window
}

func (bv *blkVerifier) SetHeight(height uint64) { bv.InternalState.SetHeight(height) }

func (bv *blkVerifier) GetStatefulBlock(blkID ids.ID) (stateful.Block, error) {
	// If block is in memory, return it.
	if blk, exists := bv.verifiedBlksCache[blkID]; exists {
		return blk, nil
	}

	statelessBlk, blkStatus, err := bv.InternalState.GetStatelessBlock(blkID)
	if err != nil {
		return nil, err
	}
	return stateful.MakeStateful(statelessBlk, bv, bv.Backend, blkStatus)
}

func (bv *blkVerifier) GetState() state.State { return bv.InternalState }

func (bv *blkVerifier) GetChainState() state.Chain { return bv.InternalState }

func (bv *blkVerifier) CacheVerifiedBlock(blk stateful.Block) {
	bv.verifiedBlksCache[blk.ID()] = blk
}

func (bv *blkVerifier) DropVerifiedBlock(blkID ids.ID) {
	delete(bv.verifiedBlksCache, blkID)
}

func (bv *blkVerifier) GetRecentlyAcceptedWindows() *window.Window {
	return bv.recentlyAccepted
}

func (bv *blkVerifier) AddToRecentlyAcceptedWindows(blkID ids.ID) {
	bv.recentlyAccepted.Add(blkID)
}

*/
