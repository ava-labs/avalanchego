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
	"github.com/ava-labs/avalanchego/utils/hashing"
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
		lc.RegisterType(&block.CoreSummaryKey{}),
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

func (vm *VM) StateSyncGetKey(summary common.Summary) (common.Key, error) {
	if _, ok := vm.ChainVM.(block.StateSyncableVM); !ok {
		return common.Key{}, common.ErrStateSyncableVMNotImplemented
	}

	proContent := block.ProposerSummaryContent{}
	ver, err := stateSyncCodec.Unmarshal(summary.Content, &proContent)
	if err != nil {
		return common.Key{}, fmt.Errorf("could not unmarshal ProposerSummaryContent due to: %w", err)
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return common.Key{}, errWrongStateSyncVersion
	}

	coreSummaryID, err := ids.ToID(hashing.ComputeHash256(proContent.CoreContent.Content))
	if err != nil {
		return common.Key{}, fmt.Errorf("could not compute core summary ID due to: %w", err)
	}
	proSummaryID, err := ids.ToID(hashing.ComputeHash256(summary.Content))
	if err != nil {
		return common.Key{}, fmt.Errorf("could not compute pro summary ID due to: %w", err)
	}

	proKey := block.ProposerSummaryKey{
		ProBlkID:      proContent.ProBlkID,
		CoreSummaryID: coreSummaryID,
		ProSummaryID:  proSummaryID,
	}
	proKeyBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proKey)
	if err != nil {
		return common.Key{}, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}

	return common.Key{Content: proKeyBytes}, nil
}

func (vm *VM) StateSyncGetLastSummary() (common.Summary, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.Summary{}, common.ErrStateSyncableVMNotImplemented
	}

	// Extract core last state summary
	vmSummary, err := ssVM.StateSyncGetLastSummary()
	if err != nil {
		return common.Summary{}, err
	}
	coreContent := block.CoreSummaryContent{}
	ver, err := stateSyncCodec.Unmarshal(vmSummary.Content, &coreContent)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot unmarshal vmSummary.Key due to: %w", err)
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return common.Summary{}, errWrongStateSyncVersion
	}

	// retrieve proposer Block wrapping coreBlock
	coreBlk, err := vm.ChainVM.GetBlock(coreContent.BlkID)
	if err != nil {
		return common.Summary{}, errWrongStateSyncVersion
	}
	proBlkID, err := vm.GetBlockIDAtHeight(coreBlk.Height())
	if err == database.ErrNotFound {
		// we must have hit the snowman++ fork. Check it.
		currentFork, err := vm.State.GetForkHeight()
		if err != nil {
			return common.Summary{}, err
		}
		if coreBlk.Height() > currentFork {
			return common.Summary{}, err
		}

		proBlkID = coreContent.BlkID
	}
	if err != nil {
		return common.Summary{}, err
	}

	// Build ProposerSummaryContent
	proContent := block.ProposerSummaryContent{
		ProBlkID:    proBlkID,
		CoreContent: coreContent,
	}
	proContentBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proContent)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}

	return common.Summary{
		Content: proContentBytes,
	}, err
}

func (vm *VM) StateSyncGetSummary(key common.Key) (common.Summary, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.Summary{}, common.ErrStateSyncableVMNotImplemented
	}

	proKey := block.ProposerSummaryKey{}
	ver, err := stateSyncCodec.Unmarshal(key.Content, &proKey)
	if err != nil {
		return common.Summary{}, err
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return common.Summary{}, errWrongStateSyncVersion
	}

	// retrieve coreBlkID
	var coreBlkID ids.ID
	switch proBlk, err := vm.getPostForkBlock(proKey.ProBlkID); err {
	case nil:
		coreBlkID = proBlk.getInnerBlk().ID()
	case database.ErrNotFound:
		coreBlkID = proKey.ProBlkID
	default:
		return common.Summary{}, err
	}

	coreKey := block.CoreSummaryKey{
		BlkID:     coreBlkID,
		ContentID: proKey.CoreSummaryID,
	}
	coreKeyBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &coreKey)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot marshal coreVMKey due to: %w", err)
	}

	coreSummary, err := ssVM.StateSyncGetSummary(common.Key{Content: coreKeyBytes})
	coreContent := block.CoreSummaryContent{}
	ver, err = stateSyncCodec.Unmarshal(coreSummary.Content, &coreContent)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot unmarshal vmSummary.Key due to: %w", err)
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return common.Summary{}, errWrongStateSyncVersion
	}

	proContent := block.ProposerSummaryContent{
		ProBlkID:    proKey.ProBlkID,
		CoreContent: coreContent,
	}
	proContentBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proContent)
	if err != nil {
		return common.Summary{}, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}

	return common.Summary{
		Content: proContentBytes,
	}, err
}

func (vm *VM) StateSync(accepted []common.Summary) error {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	coreSummaries := make([]common.Summary, 0, len(accepted))
	vm.pendingSummariesBlockIDMapping = make(map[ids.ID]ids.ID)
	for _, summ := range accepted {
		proContent := block.ProposerSummaryContent{}
		ver, err := stateSyncCodec.Unmarshal(summ.Content, &proContent)
		if err != nil {
			return err
		}
		if ver != block.StateSyncDefaultKeysVersion {
			return errWrongStateSyncVersion
		}

		coreSummaries = append(coreSummaries, common.Summary{
			Content: summ.Content,
		})

		// record coreVm to proposerVM blockID mapping to be able to
		// complete state-sync by requesting lastSummaryBlockID.
		vm.pendingSummariesBlockIDMapping[proContent.CoreContent.BlkID] = proContent.ProBlkID
	}

	return ssVM.StateSync(coreSummaries)
}

func (vm *VM) GetLastSummaryBlockID() (ids.ID, error) {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return ids.Empty, common.ErrStateSyncableVMNotImplemented
	}

	coreBlkID, err := ssVM.GetLastSummaryBlockID()
	if err != nil {
		return ids.Empty, err
	}
	proBlkID, found := vm.pendingSummariesBlockIDMapping[coreBlkID]
	vm.ctx.Log.Info("coreToProBlkID mapping found %v", proBlkID.String())
	if !found {
		return ids.Empty, errUnknownLastSummaryBlockID
	}
	return proBlkID, nil
}

func (vm *VM) SetLastSummaryBlock(blkByte []byte) error {
	ssVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return common.ErrStateSyncableVMNotImplemented
	}

	// retrieve core block
	var (
		coreBlkBytes []byte
		blk          Block
		err          error
	)
	if blk, err = vm.parsePostForkBlock(blkByte); err == nil {
		coreBlkBytes = blk.getInnerBlk().Bytes()
	} else if blk, err = vm.parsePreForkBlock(blkByte); err == nil {
		coreBlkBytes = blk.Bytes()
	} else {
		return errBadLastSummaryBlock
	}

	if err := ssVM.SetLastSummaryBlock(coreBlkBytes); err != nil {
		return err
	}

	return blk.conditionalAccept(false /*acceptcoreBlk*/)
}
