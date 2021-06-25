// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

// ProposerBlock is a decorator for a snowman.Block, created to handle block headers introduced with snowman++
// ProposerBlock is made up of a ProposerBlockHeader, carrying all the new fields introduced with snowman++, and
// a core block, which is a snowman.Block.
// ProposerBlock serialization is a two step process: the header is serialized at proposervm level, while core block
// serialization is deferred to the core VM. The structure marshallingProposerBLock encapsulates
// the serialization logic
// Contract:
// * Parent ProposerBlock wraps Parent CoreBlock of CoreBlock wrapped into Child ProposerBlock.
// * Only one call to each coreBlock's Verify() is issued from proposerVM. However Verify is memory only, so we won't persist
// core blocks over which Verify has been called
// * VERIFY FAILS ON GENESIS TODO: fix maybe
// * Rejection of ProposerBlock does not constitute Rejection of wrapped CoreBlock

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const (
	// allowable block issuance in the future
	syncBound = 10 * time.Second
)

var (
	ErrInnerBlockNotOracle = errors.New("core snowman block does not implement snowman.OracleBlock")
	ErrProBlkWrongParent   = errors.New("proposer block's parent does not wrap proposer block's core block's parent")
	ErrProBlkBadTimestamp  = errors.New("proposer block timestamp outside tolerance window")
	ErrInvalidSignature    = errors.New("proposer block signature does not verify")
	ErrProBlkWrongHeight   = errors.New("proposer block has wrong height")
	ErrFork                = errors.New("proposer block fork not acceptable")
)

type ProposerBlock struct {
	block.Block

	vm      *VM
	coreBlk snowman.Block
	status  choices.Status
}

func (pb *ProposerBlock) Accept() error {
	pb.status = choices.Accepted
	blkID := pb.ID()
	if err := pb.vm.State.SetLastAccepted(blkID); err != nil {
		return err
	}
	if err := pb.vm.storeProposerBlock(pb); err != nil {
		return err
	}

	// mark the inner block as accepted and all the conflicting inner blocks as
	// rejected
	if err := pb.vm.Tree.Accept(pb.coreBlk); err != nil {
		return err
	}

	delete(pb.vm.verifiedBlocks, blkID)
	return nil
}

func (pb *ProposerBlock) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.
	pb.status = choices.Rejected
	if err := pb.vm.storeProposerBlock(pb); err != nil {
		return err
	}

	delete(pb.vm.verifiedBlocks, pb.ID())
	return nil
}

func (pb *ProposerBlock) Status() choices.Status {
	return pb.status
}

// snowman.Block interface implementation
func (pb *ProposerBlock) Parent() snowman.Block {
	parentID := pb.ParentID()
	if res, err := pb.vm.GetBlock(parentID); err == nil {
		return res
	}
	return &missing.Block{BlkID: parentID}
}

func (pb *ProposerBlock) Verify() error {
	parent, err := pb.vm.GetBlock(pb.ParentID())
	if err != nil {
		return ErrProBlkWrongParent
	}

	// validate fork
	if parent.Timestamp().Before(pb.vm.activationTime) {
		if !pb.IsPreFork() {
			return ErrFork
		}
	} else if pb.IsPreFork() {
		return ErrFork
	}

	if !pb.IsPreFork() {
		pChainHeight := pb.PChainHeight()
		coreParentID := pb.coreBlk.Parent().ID()

		// validate parent
		if proposerParent, ok := parent.(*ProposerBlock); ok {
			if proposerParent.coreBlk.ID() != coreParentID {
				return ErrProBlkWrongParent
			}
			if pChainHeight < proposerParent.PChainHeight() {
				return ErrProBlkWrongHeight
			}
		} else if parent.ID() != coreParentID {
			return ErrProBlkWrongParent
		}

		// validate timestamp
		timestamp := pb.Timestamp()
		parentTimestamp := parent.Timestamp()

		if timestamp.Before(parentTimestamp) {
			return ErrProBlkBadTimestamp
		}

		maxTimestamp := pb.vm.Time().Add(syncBound)
		if timestamp.After(maxTimestamp) {
			return ErrProBlkBadTimestamp // too much in the future
		}

		height := pb.coreBlk.Height()
		nodeID := pb.Proposer()

		// [Delay] will return an error if [pChainHeight] is greater than the
		// current P-chain height
		minDelay, err := pb.vm.Windower.Delay(height, pChainHeight, nodeID)
		if err != nil {
			return err
		}

		minTimestamp := parentTimestamp.Add(minDelay)
		if timestamp.Before(minTimestamp) {
			return ErrProBlkBadTimestamp
		}

		if err := pb.Block.Verify(); err != nil {
			return ErrInvalidSignature
		}
	}

	// validate core block, only once
	if !pb.vm.Tree.Contains(pb.coreBlk) {
		if err := pb.coreBlk.Verify(); err != nil {
			return err
		}
		pb.vm.Tree.Add(pb.coreBlk)
	}

	pb.vm.verifiedBlocks[pb.ID()] = pb
	return nil
}

func (pb *ProposerBlock) Height() uint64 {
	return pb.coreBlk.Height()
}

// snowman.OracleBlock interface implementation
func (pb *ProposerBlock) Options() ([2]snowman.Block, error) {
	if oracleBlk, ok := pb.coreBlk.(snowman.OracleBlock); ok {
		return oracleBlk.Options()
	}

	return [2]snowman.Block{}, ErrInnerBlockNotOracle
}
