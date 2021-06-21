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

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/components/missing"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

const (
	BlkSubmissionTolerance = 10 * time.Second
	BlkSubmissionWinLength = 2 * time.Second
	proBlkVersion          = 0
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
	_, err := pb.vm.state.getLastAcceptedID()
	switch err {
	case nil:
		if err := pb.vm.state.storeLastAcceptedID(pb.ID()); err != nil {
			return err
		}
		if err := pb.coreBlk.Accept(); err != nil {
			// TODO: attempt to restore previous accepted block and return
			return err
		}

		pb.status = choices.Accepted
		if err := pb.vm.state.storeProBlk(pb); err != nil {
			return err
		}

		if err := pb.vm.propagateStatusFrom(pb); err != nil {
			// TODO: attempt to restore previous accepted block and return
			return err
		}

		delete(pb.vm.proBlkTree, pb.ParentID())
		if _, found := pb.vm.proBlkTree[pb.ID()]; !found {
			pb.vm.proBlkTree[pb.ID()] = proBlkTreeNode{
				proChildren:   make([]*ProposerBlock, 0),
				verifiedCores: make(map[ids.ID]struct{}),
			}
		}

		// pb parent block should not be needed anymore.
		// TODO: consider pruning option
		pb.vm.state.wipeFromCacheProBlk(pb.ParentID())
		return nil
	case ErrLastAcceptedIDNotFound: // pre snowman++ case
		return pb.coreBlk.Accept()
	default:
		return err
	}
}

func (pb *ProposerBlock) Reject() error {
	// coreBlock rejection is handled upon accept of siblings
	if pb.status == choices.Rejected {
		return nil // no-op
	}

	pb.status = choices.Rejected
	if err := pb.vm.state.storeProBlk(pb); err != nil {
		return err
	}
	return nil
}

func (pb *ProposerBlock) coreReject() error {
	if err := pb.coreBlk.Reject(); err != nil {
		return err
	}
	pb.vm.state.wipeFromCacheProBlk(pb.ID())
	return nil
}

func (pb *ProposerBlock) Status() choices.Status {
	return pb.status
}

// snowman.Block interface implementation
func (pb *ProposerBlock) Parent() snowman.Block {
	parentID := pb.ParentID()
	if res, err := pb.vm.state.getProBlock(parentID); err == nil {
		return res
	}

	return &missing.Block{BlkID: parentID}
}

func (pb *ProposerBlock) Verify() error {
	// validate parent
	prntBlk, err := pb.vm.state.getProBlock(pb.ParentID())
	if err != nil {
		return ErrProBlkWrongParent
	}

	if prntBlk.coreBlk.ID() != pb.coreBlk.Parent().ID() {
		return ErrProBlkWrongParent
	}

	pChainHeight := pb.PChainHeight()

	// validate height
	if pChainHeight < prntBlk.PChainHeight() {
		return ErrProBlkWrongHeight
	}

	if h, err := pb.vm.pChainHeight(); err != nil || pChainHeight > h {
		return ErrProBlkWrongHeight
	}

	timestamp := pb.Timestamp()

	// validate timestamp
	if timestamp.Before(prntBlk.Timestamp()) {
		return ErrProBlkBadTimestamp
	}

	if timestamp.After(pb.vm.now().Add(BlkSubmissionTolerance)) {
		return ErrProBlkBadTimestamp // too much in the future
	}

	nodeID := pb.Proposer()

	blkWinDelay := pb.vm.BlkSubmissionDelay(pChainHeight, nodeID)
	blkWinStart := timestamp.Add(blkWinDelay)
	if timestamp.Before(blkWinStart) {
		return ErrProBlkBadTimestamp
	}

	if err := pb.Block.Verify(); err != nil {
		return ErrInvalidSignature
	}

	// validate core block, only once
	verifiedCores := pb.vm.proBlkTree[prntBlk.ID()].verifiedCores
	if _, verified := verifiedCores[pb.coreBlk.ID()]; !verified {
		if err := pb.coreBlk.Verify(); err != nil {
			return err
		}

		verifiedCores[pb.coreBlk.ID()] = struct{}{}
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
