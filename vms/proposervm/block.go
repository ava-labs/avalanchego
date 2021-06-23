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

	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

var (
	ErrInnerBlockNotOracle     = errors.New("core snowman block does not implement snowman.OracleBlock")
	ErrProBlkWrongVersion      = errors.New("proposer block has unsupported version")
	ErrProBlkNotFound          = errors.New("proposer block not found")
	ErrProBlkWrongParent       = errors.New("proposer block's parent does not wrap proposer block's core block's parent")
	ErrProBlkBadTimestamp      = errors.New("proposer block timestamp outside tolerance window")
	ErrInvalidTLSKey           = errors.New("invalid validator signing key")
	ErrInvalidNodeID           = errors.New("could not retrieve nodeID from proposer block certificate")
	ErrInvalidSignature        = errors.New("proposer block signature does not verify")
	ErrProBlkWrongHeight       = errors.New("proposer block has wrong height")
	ErrProBlkFailedParsing     = errors.New("could not parse proposer block")
	ErrFailedHandlingConflicts = errors.New("could not handle conflict on accept")
)

type ProposerBlock struct {
	block.Block

	vm      *VM
	coreBlk snowman.Block
	status  choices.Status
}

func (pb *ProposerBlock) Accept() error {
	pb.status = choices.Accepted
	if err := pb.vm.State.PutBlock(pb.Block, choices.Accepted); err != nil {
		return err
	}
	if err := pb.vm.State.SetLastAccepted(pb.ID()); err != nil {
		return err
	}
	if err := pb.vm.db.Commit(); err != nil {
		return err
	}
	if err := pb.vm.Tree.Accept(pb.coreBlk); err != nil {
		return err
	}

	// pb parent block should not be needed anymore.
	// TODO: consider pruning option. This is only possible after fast-sync is
	// implemented and is the standard way to sync

	// reschedule for next windows
	pChainHeight, err := pb.vm.PChainHeight()
	if err != nil {
		return err
	}

	nodeID := pb.Proposer()
	blkWinDelay, err := pb.vm.Windower.Delay(pb.coreBlk.Height(), pChainHeight, nodeID)
	if err != nil {
		return err
	}
	nextBlkWinStart := pb.Timestamp().Add(blkWinDelay)
	pb.vm.scheduler.newAcceptedBlk <- nextBlkWinStart
	return nil
}

func (pb *ProposerBlock) Reject() error {
	// we do not reject the inner block here because that block may be contained
	// in the proposer block that causing this block to be rejected.
	pb.status = choices.Rejected
	if err := pb.vm.State.PutBlock(pb.Block, choices.Rejected); err != nil {
		return err
	}
	return pb.vm.db.Commit()
}

func (pb *ProposerBlock) Status() choices.Status {
	return pb.status
}

// snowman.Block interface implementation
func (pb *ProposerBlock) Parent() snowman.Block {
	parentID := pb.ParentID()
	if blk, err := pb.vm.GetBlock(parentID); err != nil {
		if _, ok := blk.(*ProposerBlock); ok {
			return blk.Parent()
		}
	}

	return &missing.Block{BlkID: parentID}
}

func (pb *ProposerBlock) Verify() error {
	// validate parent
	parent, err := pb.vm.GetBlock(pb.ParentID())
	if err != nil {
		return ErrProBlkWrongParent
	}

	proParent, ok := parent.(*ProposerBlock)
	if !ok {
		return ErrProBlkWrongParent
	}

	if proParent.coreBlk.ID() != pb.coreBlk.Parent().ID() {
		return ErrProBlkWrongParent
	}

	pChainHeight := pb.PChainHeight()

	// validate height
	if pChainHeight < proParent.PChainHeight() {
		return ErrProBlkWrongHeight
	}

	timestamp := pb.Timestamp()
	parentTimestamp := parent.Timestamp()

	// validate timestamp
	if timestamp.Before(parentTimestamp) {
		return ErrProBlkBadTimestamp
	}

	if timestamp.After(pb.vm.now().Add(proposer.MaxDelay)) {
		return ErrProBlkBadTimestamp // too much in the future
	}

	nodeID := pb.Proposer()

	blkWinDelay, err := pb.vm.Windower.Delay(pb.coreBlk.Height(), pChainHeight, nodeID)
	if err != nil {
		return err
	}
	blkWinStart := parentTimestamp.Add(blkWinDelay)
	if timestamp.Before(blkWinStart) {
		return ErrProBlkBadTimestamp
	}

	if err := pb.Block.Verify(); err != nil {
		return ErrInvalidSignature
	}

	// validate core block, only once
	if !pb.vm.Tree.Contains(pb.coreBlk) {
		if err := pb.coreBlk.Verify(); err != nil {
			return err
		}
		pb.vm.Tree.Add(pb.coreBlk)
	}
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
