// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package proposervm

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	stateSyncCodec               codec.Manager
	errWrongStateSyncVersion     = errors.New("wrong state sync key version")
	errUnknownLastSummaryBlockID = errors.New("could not retrieve blockID associated with last summary")
	errBadLastSummaryBlock       = errors.New("could not parse last summary block")
)

func init() {
	lc := linearcodec.New(reflectcodec.DefaultTagName, math.MaxUint32)
	stateSyncCodec = codec.NewManager(math.MaxInt32)

	err := stateSyncCodec.RegisterCodec(block.StateSyncDefaultKeysVersion, lc)
	if err != nil {
		panic(err)
	}
}

// Upon initialization, repairInnerBlocksMapping ensure the innerBlkID -> proBlkID
// mapping is well formed. This mapping is key for operations on summary key.
func (vm *VM) repairInnerBlocksMapping() error {
	var (
		latestProBlkID   ids.ID
		latestInnerBlkID ids.ID
		lastInnerBlk     snowman.Block
		err              error
	)

	latestProBlkID, err = vm.GetLastAccepted()
	switch err {
	case nil:
	case database.ErrNotFound:
		return nil // empty chain, nothing to do
	default:
		return err
	}

	for {
		lastAcceptedBlk, err := vm.getPostForkBlock(latestProBlkID)
		switch err {
		case nil:
		case database.ErrNotFound:
			// visited all proposerVM blocks.
			goto checkFork
		default:
			return err
		}

		latestInnerBlkID = lastAcceptedBlk.getInnerBlk().ID()
		lastInnerBlk, err = vm.ChainVM.GetBlock(latestInnerBlkID)
		if err != nil {
			// innerVM internal error
			return err
		}

		_, err = vm.State.GetBlockIDByHeight(lastInnerBlk.Height())
		switch err {
		case nil:
			// mapping already there; It must be the same for all ancestors too. Work done
			return vm.db.Commit()
		case database.ErrNotFound:
			// add the mapping
			if err := vm.State.SetBlocksIDByHeight(lastInnerBlk.Height(), latestProBlkID); err != nil {
				return err
			}

			// keep checking the parent
			latestProBlkID = lastAcceptedBlk.Parent()
		default:
			return err
		}
	}

checkFork: // handle possible snowman++ fork and commit all
	lastInnerBlk, err = vm.ChainVM.GetBlock(lastInnerBlk.Parent())
	switch err {
	case nil:
		// this is the fork. Note and commit.
		vm.forkHeight = lastInnerBlk.Height()
		return vm.db.Commit()
	case database.ErrNotFound:
		// we must have hit genesis in both proposerVM and innerVM. Work done
		return vm.db.Commit()
	default:
		return err
	}
}

func (vm *VM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() (block.Summary, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.Summary{}, block.ErrStateSyncableVMNotImplemented
	}

	vmSummary, err := fsVM.StateSyncGetLastSummary()
	if err != nil {
		return block.Summary{}, err
	}

	// Extract innerBlkID from summary key
	innerKey := block.DefaultSummaryKey{}
	parsedVersion, err := stateSyncCodec.Unmarshal(vmSummary.Key, &innerKey)
	if err != nil {
		return block.Summary{}, err
	}
	if parsedVersion != block.StateSyncDefaultKeysVersion {
		return block.Summary{}, errWrongStateSyncVersion
	}

	// retrieve proposer Block wrapping innerBlock
	innerBlk, err := vm.ChainVM.GetBlock(innerKey.InnerBlkID)
	if err != nil {
		// innerVM internal error. Could retrieve innerBlk matching last summary
		return block.Summary{}, errWrongStateSyncVersion
	}

	proBlkID, err := vm.GetBlockIDByHeight(innerBlk.Height())
	switch err {
	case nil:
	case database.ErrNotFound:
		// we must have hit the snowman++ fork. Check it.
		innerBlk, err := vm.ChainVM.GetBlock(innerKey.InnerBlkID)
		if err != nil {
			return block.Summary{}, err
		}
		if innerBlk.Height() > vm.forkHeight {
			return block.Summary{}, err
		}

		// preFork blockID matched inner ones
		proBlkID = innerKey.InnerBlkID
	default:
		return block.Summary{}, err
	}

	// recreate key
	proKey := block.ProposerSummaryKey{
		ProBlkID: proBlkID,
		InnerKey: innerKey,
	}
	proKeyBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proKey)
	if err != nil {
		return block.Summary{}, err
	}

	return block.Summary{
		Key:   proKeyBytes,
		State: vmSummary.State,
	}, err
}

func (vm *VM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	// Extract innerKey from summary key
	proKey := block.ProposerSummaryKey{}
	parsedVersion, err := stateSyncCodec.Unmarshal(key, &proKey)
	if err != nil {
		return false, err
	}
	if parsedVersion != block.StateSyncDefaultKeysVersion {
		return false, errWrongStateSyncVersion
	}

	innerKey, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, proKey.InnerKey)
	if err != nil {
		return false, err
	}

	// propagate request to innerVm
	return fsVM.StateSyncIsSummaryAccepted(innerKey)
}

func (vm *VM) StateSync(accepted []block.Summary) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	// retrieve innerKey for each summary and propagate all to innerVM
	innerSummaries := make([]block.Summary, 0, len(accepted))
	for _, summ := range accepted {
		proKey := block.ProposerSummaryKey{}
		parsedVersion, err := stateSyncCodec.Unmarshal(summ.Key, &proKey)
		if err != nil {
			return err
		}
		if parsedVersion != block.StateSyncDefaultKeysVersion {
			return errWrongStateSyncVersion
		}

		innerKey, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, proKey.InnerKey)
		if err != nil {
			return err
		}

		innerSummaries = append(innerSummaries, block.Summary{
			Key:   innerKey,
			State: summ.State,
		})
	}

	return fsVM.StateSync(innerSummaries)
}

func (vm *VM) GetLastSummaryBlockID() (ids.ID, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return ids.Empty, block.ErrStateSyncableVMNotImplemented
	}

	innerBlkID, err := fsVM.GetLastSummaryBlockID()
	if err != nil {
		return ids.Empty, err
	}
	innerBlk, err := vm.ChainVM.GetBlock(innerBlkID)
	if err != nil {
		// innerVM internal error. Could retrieve innerBlk matching last summary
		return ids.Empty, err
	}

	proBlkID, err := vm.GetBlockIDByHeight(innerBlk.Height())
	if err != nil {
		return ids.Empty, errUnknownLastSummaryBlockID
	}

	return proBlkID, nil
}

func (vm *VM) SetLastSummaryBlock(blkByte []byte) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}

	// retrieve inner block
	var (
		innerBlkBytes []byte
		blk           Block
		err           error
	)
	if blk, err = vm.parsePostForkBlock(blkByte); err == nil {
		innerBlkBytes = blk.getInnerBlk().Bytes()
	} else if blk, err = vm.parsePreForkBlock(blkByte); err == nil {
		innerBlkBytes = blk.Bytes()
	} else {
		return errBadLastSummaryBlock
	}

	// TODO ABENEGIA: return error if innerBlk's ID does not match lastSummaryBlockID
	// TODO ABENEGIA: make idempotent (to cope with SetLastSummaryBlock inner success and outer failure)
	if err := fsVM.SetLastSummaryBlock(innerBlkBytes); err != nil {
		return err
	}

	return blk.conditionalAccept(false /*acceptInnerBlk*/)
}
