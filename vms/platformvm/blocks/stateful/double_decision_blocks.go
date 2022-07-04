// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"fmt"
)

// doubleDecisionBlock contains the accept for a pair of blocks
type doubleDecisionBlock struct {
	decisionBlock
}

func (ddb *doubleDecisionBlock) acceptParent() error {
	blkID := ddb.baseBlk.ID()
	ddb.txExecutorBackend.Ctx.Log.Verbo("Accepting block with ID %s", blkID)

	parentIntf, err := ddb.parentBlock()
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		ddb.txExecutorBackend.Ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	if err := parent.Accept(); err != nil {
		return fmt.Errorf("failed to accept parent's CommonBlock: %w", err)
	}
	ddb.verifier.AddStatelessBlock(parent, parent.Status())
	ddb.verifier.SetHeight(parent.baseBlk.Height())
	ddb.verifier.AddToRecentlyAcceptedWindows(parent.baseBlk.ID())

	return nil
}

func (ddb *doubleDecisionBlock) updateState() error {
	parentIntf, err := ddb.parentBlock()
	if err != nil {
		return err
	}

	parent, ok := parentIntf.(*ProposalBlock)
	if !ok {
		ddb.txExecutorBackend.Ctx.Log.Error("double decision block should only follow a proposal block")
		return fmt.Errorf("expected Proposal block but got %T", parentIntf)
	}

	// Update the state of the chain in the database
	ddb.onAcceptState.Apply(ddb.verifier)
	if err := ddb.verifier.Commit(); err != nil {
		return fmt.Errorf("failed to commit vm's state: %w", err)
	}

	for _, child := range ddb.children {
		child.setBaseState()
	}
	if ddb.onAcceptFunc != nil {
		ddb.onAcceptFunc()
	}

	// remove this block and its parent from memory
	parent.free()
	ddb.free()
	return nil
}
