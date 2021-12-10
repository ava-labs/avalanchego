// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	_ Block    = &AbortBlock{}
	_ decision = &AbortBlock{}
)

// AbortBlock being accepted results in the proposal of its parent (which must
// be a proposal block) being rejected.
type AbortBlock struct {
	DoubleDecisionBlock `serialize:"true"`

	wasPreferred bool
}

func (a *AbortBlock) Accept() error {
	if a.vm.bootstrapped.GetValue() {
		if a.wasPreferred {
			a.vm.metrics.numVotesWon.Inc()
		} else {
			a.vm.metrics.numVotesLost.Inc()
		}
	}
	return a.DoubleDecisionBlock.Accept()
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptState if the verification passes.
func (a *AbortBlock) Verify() error {
	blkID := a.ID()

	if err := a.DoubleDecisionBlock.Verify(); err != nil {
		return err
	}

	parentIntf, err := a.parentBlock()
	if err != nil {
		return err
	}

	// The parent of an Abort block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		return errInvalidBlockType
	}

	a.onAcceptState = parent.onAbortState
	a.timestamp = a.onAcceptState.GetTimestamp()

	a.vm.currentBlocks[blkID] = a
	parent.addChild(a)
	return nil
}

// newAbortBlock returns a new *Abort block where the block's parent, a proposal
// block, has ID [parentID]. Additionally the block will track if it was
// originally preferred or not for metrics.
func (vm *VM) newAbortBlock(parentID ids.ID, height uint64, wasPreferred bool) (*AbortBlock, error) {
	abort := &AbortBlock{
		DoubleDecisionBlock: DoubleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
			},
		},
		wasPreferred: wasPreferred,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(abort)
	bytes, err := Codec.Marshal(CodecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}
	return abort, abort.DoubleDecisionBlock.initialize(vm, bytes, choices.Processing, abort)
}
