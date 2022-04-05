// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
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

type CoreSummaryContent struct {
	BlkID   ids.ID `serialize:"true"`
	Height  uint64 `serialize:"true"`
	Content []byte `serialize:"true"`
}

type ProposerSummaryContent struct {
	ProBlkID    ids.ID             `serialize:"true"`
	CoreContent CoreSummaryContent `serialize:"true"`
}

func init() {
	lc := linearcodec.NewCustomMaxLength(math.MaxUint32)
	stateSyncCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&block.Summary{}),
		lc.RegisterType(&ids.ID{}),
		lc.RegisterType(&CoreSummaryContent{}),
		lc.RegisterType(&ProposerSummaryContent{}),
		stateSyncCodec.RegisterCodec(block.StateSyncDefaultKeysVersion, lc),
	)
	if err := errs.Err; err != nil {
		panic(err)
	}
}

func (vm *VM) StateSyncEnabled() (bool, error) {
	if vm.coreStateSyncVM == nil {
		return false, common.ErrStateSyncableVMNotImplemented
	}

	return vm.coreStateSyncVM.StateSyncEnabled()
}

func (vm *VM) StateSyncGetLastSummary() (common.Summary, error) {
	if vm.coreStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	// Extract core last state summary
	vmSummary, err := vm.coreStateSyncVM.StateSyncGetLastSummary()
	if err != nil {
		return nil, err
	}

	proContent, err := vm.buildProContentFrom(vmSummary)
	if err != nil {
		return nil, fmt.Errorf("could not build proposerVm Summary from core one due to: %w", err)
	}

	proSummBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proContent)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}
	return newSummary(vmSummary.Key(), proSummBytes)
}

func (vm *VM) ParseSummary(summaryBytes []byte) (common.Summary, error) {
	if _, ok := vm.ChainVM.(block.StateSyncableVM); !ok {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	proContent := ProposerSummaryContent{}
	ver, err := stateSyncCodec.Unmarshal(summaryBytes, &proContent)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal ProposerSummaryContent due to: %w", err)
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return nil, errWrongStateSyncVersion
	}

	return newSummary(proContent.CoreContent.Height, summaryBytes)
}

func (vm *VM) StateSyncGetSummary(key uint64) (common.Summary, error) {
	if vm.coreStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	coreSummary, err := vm.coreStateSyncVM.StateSyncGetSummary(key)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve core summary due to: %w", err)
	}
	proContent, err := vm.buildProContentFrom(coreSummary)
	if err != nil {
		return nil, fmt.Errorf("could not build proposerVm Summary from core one due to: %w", err)
	}

	proSummBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proContent)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}

	return newSummary(coreSummary.Key(), proSummBytes)
}

func (vm *VM) StateSync(accepted []common.Summary) error {
	if vm.coreStateSyncVM == nil {
		return common.ErrStateSyncableVMNotImplemented
	}

	coreSummaries := make([]common.Summary, 0, len(accepted))
	for _, summary := range accepted {
		proContent := ProposerSummaryContent{}
		ver, err := stateSyncCodec.Unmarshal(summary.Bytes(), &proContent)
		if err != nil {
			return err
		}
		if ver != block.StateSyncDefaultKeysVersion {
			return errWrongStateSyncVersion
		}

		coreSumBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, proContent.CoreContent)
		if err != nil {
			return err
		}
		coreSummary, err := newSummary(summary.Key(), coreSumBytes)
		if err != nil {
			return err
		}

		coreSummaries = append(coreSummaries, coreSummary)

		// Following state sync introduction, we update height -> blockID index
		// with summaries content in order to support resuming state sync in case
		// of shutdown. Note that we won't download all the blocks associated with
		// state summaries.
		if err := vm.updateHeightIndex(summary.Key(), proContent.ProBlkID); err != nil {
			return err
		}
	}

	return vm.coreStateSyncVM.StateSync(coreSummaries)
}

func (vm *VM) GetOngoingStateSyncSummary() (common.Summary, error) {
	if vm.coreStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	coreSummary, err := vm.coreStateSyncVM.GetOngoingStateSyncSummary()
	if err != nil {
		return nil, err // including common.ErrNoStateSyncOngoing case
	}

	proContent, err := vm.buildProContentFrom(coreSummary)
	if err != nil {
		return nil, fmt.Errorf("could not build proposerVm Summary from core one due to: %w", err)
	}

	proSummBytes, err := stateSyncCodec.Marshal(block.StateSyncDefaultKeysVersion, &proContent)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}
	return newSummary(coreSummary.Key(), proSummBytes)
}

func (vm *VM) GetStateSyncResult() (ids.ID, uint64, error) {
	if vm.coreStateSyncVM == nil {
		return ids.Empty, 0, common.ErrStateSyncableVMNotImplemented
	}

	_, height, err := vm.coreStateSyncVM.GetStateSyncResult()
	if err != nil {
		return ids.Empty, 0, err
	}
	proBlkID, err := vm.GetBlockIDAtHeight(height)
	if err != nil {
		return ids.Empty, 0, errUnknownLastSummaryBlockID
	}
	vm.ctx.Log.Info("coreToProBlkID mapping found %v", proBlkID.String())
	return proBlkID, height, nil
}

func (vm *VM) SetLastSummaryBlock(blkByte []byte) error {
	if vm.coreStateSyncVM == nil {
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

	if err := vm.coreStateSyncVM.SetLastSummaryBlock(coreBlkBytes); err != nil {
		return err
	}

	return blk.acceptOuterBlk()
}

func newSummary(key uint64, content []byte) (common.Summary, error) {
	summaryID, err := ids.ToID(hashing.ComputeHash256(content))
	if err != nil {
		return nil, fmt.Errorf("cannot compute summary ID: %w", err)
	}
	return &block.Summary{
		SummaryKey:   key,
		SummaryID:    summaryID,
		ContentBytes: content,
	}, nil
}

func (vm *VM) buildProContentFrom(coreSummary common.Summary) (ProposerSummaryContent, error) {
	coreContent := CoreSummaryContent{}
	ver, err := stateSyncCodec.Unmarshal(coreSummary.Bytes(), &coreContent)
	if err != nil {
		return ProposerSummaryContent{}, err
	}
	if ver != block.StateSyncDefaultKeysVersion {
		return ProposerSummaryContent{}, errWrongStateSyncVersion
	}

	// retrieve ProBlkID
	proBlkID, err := vm.GetBlockIDAtHeight(coreContent.Height)
	if err != nil {
		return ProposerSummaryContent{}, err
	}

	// Build ProposerSummaryContent
	return ProposerSummaryContent{
		ProBlkID:    proBlkID,
		CoreContent: coreContent,
	}, nil
}
