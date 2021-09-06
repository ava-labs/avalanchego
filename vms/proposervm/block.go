// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
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

type PostForkBlock interface {
	Block

	setStatus(choices.Status)
	getStatelessBlk() block.Block
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
// 1) [p]'s inner block is not an oracle block
// 2) [child]'s P-Chain height >= [parentPChainHeight]
// 3) [p]'s inner block is the parent of [c]'s inner block
// 4) [child]'s timestamp isn't before [p]'s timestamp
// 5) [child]'s timestamp is within the skew bound
// 6) [childPChainHeight] <= the current P-Chain height
// 7) [child]'s timestamp is within its proposer's window
// 8) [child] has a valid signature from its proposer
// 9) [child]'s inner block is valid
func (p *postForkCommonComponents) Verify(parentTimestamp time.Time, parentPChainHeight uint64, child *postForkBlock) error {
	if err := verifyIsNotOracleBlock(p.innerBlk); err != nil {
		return err
	}

	childPChainHeight := child.PChainHeight()
	if childPChainHeight < parentPChainHeight {
		return errPChainHeightNotMonotonic
	}

	expectedInnerParentID := p.innerBlk.ID()
	innerParentID := child.innerBlk.Parent()
	if innerParentID != expectedInnerParentID {
		return errInnerParentMismatch
	}

	childTimestamp := child.Timestamp()
	if childTimestamp.Before(parentTimestamp) {
		return errTimeNotMonotonic
	}

	maxTimestamp := p.vm.Time().Add(maxSkew)
	if childTimestamp.After(maxTimestamp) {
		return errTimeTooAdvanced
	}

	// If the node is currently bootstrapping - we don't assume that the P-chain
	// has been synced up to this point yet.
	if p.vm.ctx.IsBootstrapped() {
		childID := child.ID()
		currentPChainHeight, err := p.vm.ctx.ValidatorState.GetCurrentHeight()
		if err != nil {
			p.vm.ctx.Log.Error("failed to get current P-Chain height while processing %s: %s",
				childID, err)
			return err
		}
		if childPChainHeight > currentPChainHeight {
			return errPChainHeightNotReached
		}

		childHeight := child.Height()
		proposerID := child.Proposer()
		minDelay, err := p.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
		if err != nil {
			return err
		}

		delay := childTimestamp.Sub(parentTimestamp)
		if delay < minDelay {
			return errProposerWindowNotStarted
		}

		// Verify the signature of the node
		shouldHaveProposer := delay < proposer.MaxDelay
		if err := child.SignedBlock.Verify(shouldHaveProposer, p.vm.ctx.ChainID); err != nil {
			return err
		}

		p.vm.ctx.Log.Debug("verified post-fork block %s - parent timestamp %v, expected delay %v, block timestamp %v",
			childID, parentTimestamp, minDelay, childTimestamp)
	}

	return p.vm.verifyAndRecordInnerBlk(child)
}

// Return the child (a *postForkBlock) of this block
func (p *postForkCommonComponents) buildChild(
	parentID ids.ID,
	parentTimestamp time.Time,
	parentPChainHeight uint64,
	innerBlock snowman.Block,
) (Block, error) {
	// Child's timestamp is the later of now and this block's timestamp
	newTimestamp := p.vm.Time().Truncate(time.Second)
	if newTimestamp.Before(parentTimestamp) {
		newTimestamp = parentTimestamp
	}

	// The child's P-Chain height is the P-Chain's height when it was proposed
	// (i.e. now)
	pChainHeight, err := p.vm.ctx.ValidatorState.GetCurrentHeight()
	if err != nil {
		return nil, err
	}

	delay := newTimestamp.Sub(parentTimestamp)

	// Build the child
	var statelessChild block.SignedBlock
	if delay >= proposer.MaxDelay {
		statelessChild, err = block.BuildUnsigned(
			parentID,
			newTimestamp,
			pChainHeight,
			innerBlock.Bytes(),
		)
		if err != nil {
			return nil, err
		}
	} else {
		// The following [minTimestamp] check should be able to be removed, but
		// this is left here as a sanity check
		childHeight := innerBlock.Height()
		proposerID := p.vm.ctx.NodeID
		minDelay, err := p.vm.Windower.Delay(childHeight, parentPChainHeight, proposerID)
		if err != nil {
			return nil, err
		}

		if delay < minDelay {
			// It's not our turn to propose a block yet
			p.vm.ctx.Log.Warn("build block dropped; parent timestamp %s, expected delay %s, block timestamp %s",
				parentTimestamp, minDelay, newTimestamp)
			return nil, errProposerWindowNotStarted
		}

		statelessChild, err = block.Build(
			parentID,
			newTimestamp,
			pChainHeight,
			p.vm.ctx.StakingCertLeaf,
			innerBlock.Bytes(),
			p.vm.ctx.ChainID,
			p.vm.ctx.StakingLeafSigner,
		)
		if err != nil {
			return nil, err
		}
	}

	child := &postForkBlock{
		SignedBlock: statelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       p.vm,
			innerBlk: innerBlock,
			status:   choices.Processing,
		},
	}

	p.vm.ctx.Log.Info("built block %s - parent timestamp %v, block timestamp %v",
		child.ID(), parentTimestamp, newTimestamp)
	// Persist the child
	return child, p.vm.storePostForkBlock(child)
}

func verifyIsOracleBlock(b snowman.Block) error {
	oracle, ok := b.(snowman.OracleBlock)
	if !ok {
		return errUnexpectedBlockType
	}
	_, err := oracle.Options()
	return err
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
