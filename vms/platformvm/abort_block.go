// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

var (
	_ Block    = &Abort{}
	_ decision = &Abort{}
)

// Abort being accepted results in the proposal of its parent (which must be a proposal block)
// being rejected.
type Abort struct {
	DoubleDecisionBlock `serialize:"true"`
}

// Verify this block performs a valid state transition.
//
// The parent block must be a proposal
//
// This function also sets onAcceptDB database if the verification passes.
func (a *Abort) Verify() error {
	blkID := a.ID()

	if err := a.DoubleDecisionBlock.Verify(); err != nil {
		if err := a.Reject(); err != nil {
			a.vm.ctx.Log.Error("failed to reject abort block %s due to %s", blkID, err)
		}
		return err
	}

	parentIntf, err := a.parent()
	if err != nil {
		return err
	}

	// The parent of an Abort block should always be a proposal
	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		if err := a.Reject(); err != nil {
			a.vm.ctx.Log.Error("failed to reject abort block %s due to %s", blkID, err)
		}
		return errInvalidBlockType
	}

	a.onAcceptState, a.onAcceptFunc = parent.onAbort()

	a.vm.currentBlocks[blkID] = a
	parentIntf.addChild(a)
	return nil
}

// newAbortBlock returns a new *Abort block where the block's parent, a proposal
// block, has ID [parentID].
func (vm *VM) newAbortBlock(parentID ids.ID, height uint64) (*Abort, error) {
	abort := &Abort{
		DoubleDecisionBlock: DoubleDecisionBlock{
			CommonDecisionBlock: CommonDecisionBlock{
				CommonBlock: CommonBlock{
					PrntID: parentID,
					Hght:   height,
				},
			},
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(abort)
	bytes, err := Codec.Marshal(codecVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}
	return abort, abort.DoubleDecisionBlock.initialize(vm, bytes, choices.Processing, abort)
}
