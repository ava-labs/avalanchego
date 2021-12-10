// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package proposervm

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/codec/reflectcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"
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

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&common.Summary{}),
		lc.RegisterType(&block.DefaultSummaryKey{}),
		stateSyncCodec.RegisterCodec(block.StateSyncDefaultKeysVersion, lc),
	)
	if err := errs.Err; err != nil {
		panic(err)
	}
}

func (vm *VM) RegisterFastSyncer(fastSyncers []ids.ShortID) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	return fsVM.RegisterFastSyncer(fastSyncers)
}

func (vm *VM) StateSyncEnabled() (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	return fsVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() (common.Summary, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.Summary{}, common.ErrStateSyncableVMNotImplemented
	}

	vmSummary, err := fsVM.StateSyncGetLastSummary()
	if err != nil {
		return common.Summary{}, err
	}

	// Extract innerBlkID from summary key
	innerKey := block.DefaultSummaryKey{}
	parsedVersion, err := stateSyncCodec.Unmarshal(vmSummary.Key, &innerKey)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot unmarshal vmSummary.Key due to: %w", err)
	}
	if parsedVersion != block.StateSyncDefaultKeysVersion {
		return common.Summary{}, errWrongStateSyncVersion
	}

	// retrieve proposer Block wrapping innerBlock
	innerBlk, err := vm.ChainVM.GetBlock(innerKey.InnerBlkID)
	if err != nil {
		// innerVM internal error. Could retrieve innerBlk matching last summary
		return common.Summary{}, errWrongStateSyncVersion
	}

	proBlkID, err := vm.GetBlockIDByHeight(innerBlk.Height())
	switch err {
	case nil:
	case database.ErrNotFound:
		// we must have hit the snowman++ fork. Check it.
		innerBlk, err := vm.ChainVM.GetBlock(innerKey.InnerBlkID)
		if err != nil {
			return common.Summary{}, err
		}
		if innerBlk.Height() > vm.latestPreForkHeight {
			return common.Summary{}, err
		}

		// preFork blockID matched inner ones
		proBlkID = innerKey.InnerBlkID
	default:
		return common.Summary{}, err
	}

	// recreate key
	proKey := block.ProposerSummaryKey{
		ProBlkID: proBlkID,
		InnerKey: innerKey,
	}
	proKeyBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proKey)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}

	return common.Summary{
		Key:   proKeyBytes,
		State: vmSummary.State,
	}, err
}

func (vm *VM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, common.ErrStateSyncableVMNotImplemented
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

func (vm *VM) StateSync(accepted []common.Summary) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	// retrieve innerKey for each summary and propagate all to innerVM
	innerSummaries := make([]common.Summary, 0, len(accepted))
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

		innerSummaries = append(innerSummaries, common.Summary{
			Key:   innerKey,
			State: summ.State,
		})
	}

	return fsVM.StateSync(innerSummaries)
}

func (vm *VM) GetLastSummaryBlockID() (ids.ID, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return ids.Empty, common.ErrStateSyncableVMNotImplemented
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
		return common.ErrStateSyncableVMNotImplemented
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
