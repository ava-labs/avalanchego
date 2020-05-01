// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/core"
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
	parent, ok := a.parentBlock().(*ProposalBlock)
	// Abort is a decision, so its parent must be a proposal
	if !ok {
		return errInvalidBlockType
	}

	a.onAcceptDB, a.onAcceptFunc = parent.onAbort()

	a.vm.currentBlocks[a.ID().Key()] = a
	a.parentBlock().addChild(a)
	return nil
}

// newAbortBlock returns a new *Abort block where the block's parent, a proposal
// block, has ID [parentID].
func (vm *VM) newAbortBlock(parentID ids.ID) *Abort {
	abort := &Abort{DoubleDecisionBlock: DoubleDecisionBlock{CommonDecisionBlock: CommonDecisionBlock{CommonBlock: CommonBlock{
		Block: core.NewBlock(parentID),
		vm:    vm,
	}}}}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(abort)
	bytes, err := Codec.Marshal(&blk)
	if err != nil {
		return nil
	}

	abort.Block.Initialize(bytes, vm.SnowmanVM)
	return abort
}
