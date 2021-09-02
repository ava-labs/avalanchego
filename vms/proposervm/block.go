// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

const (
	// allowable block issuance in the future
	maxSkew = 10 * time.Second
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
)

type Block interface {
	snowman.Block

	getInnerBlk() snowman.Block

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
	if err := verifyIsNotOracleBlock(p.innerBlk); err != nil {
		p.vm.ctx.Log.Debug("oracle block shouldn't have a signed child")
		return err
	}

	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; expected child's P-Chain height to be >=%d but got %d",
			parentPChainHeight, childPChainHeight)
		return errPChainHeightNotMonotonic
	}

	expectedInnerParentID := p.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; expected inner parent %s but got %s",
			expectedInnerParentID, innerParentID)
		return errInnerParentMismatch
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; expected child's timestamp (%s) to be at or after parent's timestamp (%s)",
			childTimestamp, parentTimestamp)
		return errTimeNotMonotonic
	}

	maxTimestamp := p.vm.Time().Add(maxSkew)
	if childTimestamp.After(maxTimestamp) {
		p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; block's timestamp (%s) is after the synchrony bound (%s)",
			childTimestamp, maxTimestamp)
		return errTimeTooAdvanced
	}

	delay := childTimestamp.Sub(parentTimestamp)
	proposerID := child.Proposer()
	if delay >= proposer.MaxDelay && proposerID != ids.ShortEmpty {
		return errExpectedNoProposer
	}

	// If the node is currently bootstrapping - we don't assume that the P-chain
	// has been synced up to this point yet.
	if p.vm.ctx.IsBootstrapped() {
		childID := child.ID()
		currentPChainHeight, err := p.vm.PChainHeight()
		if err != nil {
			p.vm.ctx.Log.Error("Snowman++ verify - dropped post-fork block %s; could not retrieve current P-Chain height",
				childID)
			return err
		}
		if childPChainHeight > currentPChainHeight {
			p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; expected chid's P-Chain height to be <=%d but got %d",
				currentPChainHeight, childPChainHeight)
			return errPChainHeightNotReached
		}

		childHeight := child.Height()
		minDelay, err := p.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
		if err != nil {
			return err
		}

		p.vm.ctx.Log.Debug("Snowman++ verify post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v.",
			childID, parentTimestamp, minDelay, childTimestamp)

		if delay < minDelay {
			p.vm.ctx.Log.Warn("Snowman++ verify - dropped post-fork block; timestamp is %s but proposer %s%s can't propose yet",
				childTimestamp, constants.NodeIDPrefix, proposerID)
			return errProposerWindowNotStarted
		}

		// Verify the signature of the node
		if err := child.Block.Verify(); err != nil {
			return err
		}
	}

	return p.vm.verifyAndRecordInnerBlk(child)
}

func verifyIsOracleBlock(b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return errUnexpectedBlockType
	}
	_, err := oracle.Options()
	switch err {
	case nil:
		return nil
	case snowman.ErrNotOracle:
		return errUnexpectedBlockType
	default:
		return err
	}
}

func verifyIsNotOracleBlock(b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return nil
	}
	_, err := oracle.Options()
	switch err {
	case nil:
		return errUnexpectedBlockType
	case snowman.ErrNotOracle:
		return nil
	default:
		return err
	}
}
