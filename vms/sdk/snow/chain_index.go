// Copyright (C) 2024, Ava Labs, Inv. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

// ChainIndex defines the generic on-disk index for the Input block type required
// by the VM.
// ChainIndex must serve the last accepted block, it is up to the implementation
// how large of a window of accepted blocks to maintain in its index.
// The VM provides a caching layer on top of ChainIndex, so the implementation
// does not need to provide its own caching layer.
type ChainIndex[T Block] interface {
	UpdateLastAccepted(ctx context.Context, blk T) error
	GetLastAcceptedHeight(ctx context.Context) (uint64, error)
	GetBlock(ctx context.Context, blkID ids.ID) (T, error)
	GetBlockIDAtHeight(ctx context.Context, blkHeight uint64) (ids.ID, error)
	GetBlockIDHeight(ctx context.Context, blkID ids.ID) (uint64, error)
	GetBlockByHeight(ctx context.Context, blkHeight uint64) (T, error)
}

func (v *VM[I, O, A]) makeConsensusIndex(
	ctx context.Context,
	chainIndex ChainIndex[I],
	outputBlock O,
	acceptedBlock A,
	stateReady bool,
) error {
	v.inputChainIndex = chainIndex
	lastAcceptedHeight, err := v.inputChainIndex.GetLastAcceptedHeight(ctx)
	if err != nil {
		return err
	}
	inputBlock, err := v.inputChainIndex.GetBlockByHeight(ctx, lastAcceptedHeight)
	if err != nil {
		return err
	}

	var lastAcceptedBlock *StatefulBlock[I, O, A]
	if stateReady {
		v.ready = true
		lastAcceptedBlock, err = v.reprocessFromOutputToInput(ctx, inputBlock, outputBlock, acceptedBlock)
		if err != nil {
			return err
		}
	} else {
		v.ready = false
		lastAcceptedBlock = NewInputBlock(v, inputBlock)
	}
	v.setLastAccepted(lastAcceptedBlock)
	v.preferredBlkID = lastAcceptedBlock.ID()
	v.consensusIndex = &ConsensusIndex[I, O, A]{v}

	return nil
}

// GetConsensusIndex returns the consensus index exposed to the application. The consensus index is created during chain initialization
// and is exposed here for testing.
func (v *VM[I, O, A]) GetConsensusIndex() *ConsensusIndex[I, O, A] {
	return v.consensusIndex
}

// reprocessFromOutputToInput re-processes blocks from output/accepted to align with the supplied input block.
// assumes that outputBlock and acceptedBlock represent the same block and that all blocks in the range
// [output/accepted, input] have been added to the inputChainIndex.
func (v *VM[I, O, A]) reprocessFromOutputToInput(ctx context.Context, targetInputBlock I, outputBlock O, acceptedBlock A) (*StatefulBlock[I, O, A], error) {
	if targetInputBlock.GetHeight() < outputBlock.GetHeight() || outputBlock.GetID() != acceptedBlock.GetID() {
		return nil, fmt.Errorf("invalid initial accepted state (Input = %s, Output = %s, Accepted = %s)", targetInputBlock, outputBlock, acceptedBlock)
	}

	// Re-process from the last output block, to the last accepted input block
	for targetInputBlock.GetHeight() > outputBlock.GetHeight() {
		reprocessInputBlock, err := v.inputChainIndex.GetBlockByHeight(ctx, outputBlock.GetHeight()+1)
		if err != nil {
			return nil, err
		}

		outputBlock, err = v.chain.VerifyBlock(ctx, outputBlock, reprocessInputBlock)
		if err != nil {
			return nil, err
		}
		acceptedBlock, err = v.chain.AcceptBlock(ctx, acceptedBlock, outputBlock)
		if err != nil {
			return nil, err
		}
	}

	return NewAcceptedBlock(v, targetInputBlock, outputBlock, acceptedBlock), nil
}

// ConsensusIndex provides a wrapper around the VM, which enables the chain developer to share the
// caching layer provided by the VM and used in the consensus engine.
// The ConsensusIndex additionally provides access to the accepted/preferred frontier by providing
// accessors to the latest type of the frontier.
// ie. last accepted block is guaranteed to have Accepted type available, whereas the preferred block
// is only guaranteed to have the Output type available.
type ConsensusIndex[I Block, O Block, A Block] struct {
	vm *VM[I, O, A]
}

func (c *ConsensusIndex[I, O, A]) GetBlockByHeight(ctx context.Context, height uint64) (I, error) {
	blk, err := c.vm.GetBlockByHeight(ctx, height)
	if err != nil {
		return utils.Zero[I](), err
	}
	return blk.Input, nil
}

func (c *ConsensusIndex[I, O, A]) GetBlock(ctx context.Context, blkID ids.ID) (I, error) {
	blk, err := c.vm.GetBlock(ctx, blkID)
	if err != nil {
		return utils.Zero[I](), err
	}
	return blk.Input, nil
}

// GetPreferredBlock returns the output block of the current preference.
//
// Prior to dynamic state sync, GetPreferredBlock will return an error because the preference
// will not have been verified.
// After completing dynamic state sync, all outstanding processing blocks will be verified.
// However, there's an edge case where the node may have vacuously verified an invalid block
// during dynamic state sync, such that the preferred block is invalid and its output is
// empty.
// Consensus should guarantee that we do not accept such a block even if it's preferred as
// long as a majority of validators are correct.
// After outstanding processing blocks have been Accepted/Rejected, the preferred block
// will be verified and the output will be available.
func (c *ConsensusIndex[I, O, A]) GetPreferredBlock(ctx context.Context) (O, error) {
	c.vm.metaLock.Lock()
	preference := c.vm.preferredBlkID
	c.vm.metaLock.Unlock()

	blk, err := c.vm.GetBlock(ctx, preference)
	if err != nil {
		return utils.Zero[O](), err
	}
	if !blk.verified {
		return utils.Zero[O](), fmt.Errorf("preferred block %s has not been verified", blk)
	}
	return blk.Output, nil
}

// GetLastAccepted returns the last accepted block of the chain.
//
// If the chain is mid dynamic state sync, GetLastAccepted will return an error
// because the last accepted block will not be populated.
func (c *ConsensusIndex[I, O, A]) GetLastAccepted(context.Context) (A, error) {
	c.vm.metaLock.Lock()
	defer c.vm.metaLock.Unlock()

	lastAccepted := c.vm.lastAcceptedBlock

	if !lastAccepted.accepted {
		return utils.Zero[A](), fmt.Errorf("last accepted block %s has not been populated", lastAccepted)
	}
	return lastAccepted.Accepted, nil
}
