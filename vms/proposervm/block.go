// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

const (
	// allowable block issuance in the future
	syncBound = 10 * time.Second
)

var (
	errUnsignedChild            = errors.New("expected child to be signed")
	errUnexpectedBlockType      = errors.New("unexpected proposer block type")
	errInnerParentMismatch      = errors.New("inner parentID didn't match expected parent")
	errTimeNotMonotonic         = errors.New("time must monotonically increase")
	errPChainHeightNotMonotonic = errors.New("non monotonically increasing P-chain height")
	errPChainHeightNotReached   = errors.New("block P-chain height larger than current P-chain height")
	errTimeTooAdvanced          = errors.New("time is too far advanced")
	errProposerWindowNotStarted = errors.New("proposer window hasn't started")
	errProposersNotActivated    = errors.New("proposers haven't been activated yet")
	errProposersActivated       = errors.New("proposers have been activated")
)

type Block interface {
	snowman.Block

	verifyPreForkChild(child *preForkBlock) error
	verifyPostForkChild(child *postForkBlock) error
	verifyPostForkOption(child *postForkOption) error

	buildChild(innerBlock snowman.Block) (Block, error)

	pChainHeight() (uint64, error)
}

// field of postForkBlock and postForkOption
type postForkCommonComponents struct {
	vm       *VM
	innerBlk snowman.Block
	status   choices.Status
}

// Return the inner block's height
func (p *postForkCommonComponents) Height() uint64 {
	return p.innerBlk.Height()
}

// Verify returns nil if:
// 1) [child]'s P-Chain height >= [parentPChainHeight]
// 2) [childPChainHeight] <= the current P-Chain height
// 3) [p]'s inner block is the parent of [c]'s inner block
// 4) [child]'s timestamp is within the synchrony bound, after [p]'s timestamp, and within its proposer's window
// 5) [child] has a valid signature from its proposer
// 6) [child]'s inner block is valid
func (p *postForkCommonComponents) Verify(parentTimestamp time.Time, parentPChainHeight uint64, child *postForkBlock) error {
	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	childID := child.ID()
	currentPChainHeight, err := p.vm.PChainHeight()
	if err != nil {
		p.vm.ctx.Log.Error("Snowman++ verify post-fork block %s - could not retrieve current P-Chain height",
			childID)
		return err
	}
	if childPChainHeight > currentPChainHeight {
		return errPChainHeightNotReached
	}

	expectedInnerParentID := p.innerBlk.ID()
	innerParent := child.innerBlk.Parent()
	innerParentID := innerParent.ID()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := p.vm.Time().Add(syncBound)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	childHeight := child.Height()
	proposerID := child.Proposer()
	minDelay, err := p.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
	if err != nil {
		return err
	}

	minTimestamp := parentTimestamp.Add(minDelay)
	p.vm.ctx.Log.Debug("Snowman++ verify post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v.",
		childID, parentTimestamp.Format("15:04:05"), minDelay, childTimestamp.Format("15:04:05"))

	if childTimestamp.Before(minTimestamp) {
		p.vm.ctx.Log.Debug("Snowman++ verify - dropped post-fork block due to time window %s", childID)
		return errProposerWindowNotStarted
	}

	// Verify the signature of the node
	if err := child.Block.Verify(); err != nil {
		return err
	}

	// If inner block's Verify returned true, don't call it again.
	// Note that if [child.innerBlk.Verify] returns nil,
	// this method returns nil. This must always remain the case to
	// maintain the inner block's invariant that if it's Verify()
	// returns nil, it is eventually accepted/rejected.
	if !p.vm.Tree.Contains(child.innerBlk) {
		if err := child.innerBlk.Verify(); err != nil {
			return err
		}
		p.vm.Tree.Add(child.innerBlk)
	}

	p.vm.verifiedBlocks[childID] = child
	return nil
}
