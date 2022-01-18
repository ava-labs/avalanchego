// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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

func (vm *VM) RegisterStateSyncer(stateSyncers []ids.ShortID) error {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	return ssVM.RegisterStateSyncer(stateSyncers)
}

func (vm *VM) StateSyncEnabled() (bool, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	return ssVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() (common.Summary, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.Summary{}, common.ErrStateSyncableVMNotImplemented
	}

	vmSummary, err := ssVM.StateSyncGetLastSummary()
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
	innerBlk, err := vm.ChainVM.GetBlock(innerKey.BlkID)
	if err != nil {
		// innerVM internal error. Could retrieve innerBlk matching last summary
		return common.Summary{}, errWrongStateSyncVersion
	}

	proBlkID, err := vm.GetBlockIDByHeight(innerBlk.Height())
	switch err {
	case nil:
	case database.ErrNotFound:
		// we must have hit the snowman++ fork. Check it.
		currentFork, err := vm.State.GetForkHeight()
		if err != nil {
			return common.Summary{}, err
		}
		innerBlk, err := vm.ChainVM.GetBlock(innerKey.BlkID)
		if err != nil {
			return common.Summary{}, err
		}
		if innerBlk.Height() > currentFork {
			return common.Summary{}, err
		}

		// preFork blockID matched inner ones
		proBlkID = innerKey.BlkID
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
		Key:     proKeyBytes,
		Content: vmSummary.Content,
	}, err
}

func (vm *VM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
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
	return ssVM.StateSyncIsSummaryAccepted(innerKey)
}

func (vm *VM) StateSync(accepted []common.Summary) error {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	// retrieve innerKey for each summary and propagate all to innerVM
	innerSummaries := make([]common.Summary, 0, len(accepted))
	vm.pendingSummariesBlockIDMapping = make(map[ids.ID]ids.ID)
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
			Key:     innerKey,
			Content: summ.Content,
		})

		// record innerVm to proposerVM blockID mapping to be able to
		// complete fasty-sync by requesting lastSummarBlockID.
		var innerID ids.ID
		copy(innerID[:], innerKey)
		vm.pendingSummariesBlockIDMapping[innerID] = proKey.ProBlkID
	}

	return ssVM.StateSync(innerSummaries)
}

func (vm *VM) GetLastSummaryBlockID() (ids.ID, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return ids.Empty, common.ErrStateSyncableVMNotImplemented
	}

	innerBlkID, err := ssVM.GetLastSummaryBlockID()
	if err != nil {
		return ids.Empty, err
	}
	innerBlk, err := vm.ChainVM.GetBlock(innerBlkID)
	if err != nil {
		return ids.Empty, err
	}
	proBlkID, err := vm.State.GetBlockIDAtHeight(innerBlk.Height())
	if err != nil {
		// GetLastSummaryBlockID may be issued by engine itself to request
		// LastSummaryBlockID and complete fast sync. In such case the node
		// won't know yet the full block corresponding to the blockID.
		// So search among the summaries discovered by peer validators.
		found := false
		proBlkID, found = vm.pendingSummariesBlockIDMapping[innerBlkID]
		vm.ctx.Log.Info("innerToProBlkID mapping found %v", proBlkID.String())
		if !found {
			return ids.Empty, errUnknownLastSummaryBlockID
		}
	}
	return proBlkID, nil
}

func (vm *VM) SetLastSummaryBlock(blkByte []byte) error {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
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
		innerBlkBytes = blk.GetInnerBlk().Bytes()
	} else if blk, err = vm.parsePreForkBlock(blkByte); err == nil {
		innerBlkBytes = blk.Bytes()
	} else {
		return errBadLastSummaryBlock
	}

	if err := ssVM.SetLastSummaryBlock(innerBlkBytes); err != nil {
		return err
	}

	return blk.conditionalAccept(false /*acceptInnerBlk*/)
}
