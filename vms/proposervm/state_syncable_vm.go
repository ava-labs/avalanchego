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
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ common.Summary = &ProposerSummaryContent{}

	stateSyncCodec               codec.Manager
	errWrongStateSyncVersion     = errors.New("wrong state sync key version")
	errUnknownLastSummaryBlockID = errors.New("could not retrieve blockID associated with last summary")
	errBadLastSummaryBlock       = errors.New("could not parse last summary block")
)

const StateSummaryVersion = 0

type ProposerSummaryContent struct {
	ProBlkID    ids.ID `serialize:"true"`
	CoreContent []byte `serialize:"true"`

	proSummaryID ids.ID
	proContent   []byte
	key          uint64
}

func (ps *ProposerSummaryContent) Bytes() []byte { return ps.proContent }
func (ps *ProposerSummaryContent) Key() uint64   { return ps.key }
func (ps *ProposerSummaryContent) ID() ids.ID    { return ps.proSummaryID }

func newSummary(proBlkID ids.ID, coreSummary common.Summary) (common.Summary, error) {
	res := &ProposerSummaryContent{
		ProBlkID:    proBlkID,
		CoreContent: coreSummary.Bytes(),

		key: coreSummary.Key(), // note: this is not serialized
	}

	proContent, err := stateSyncCodec.Marshal(StateSummaryVersion, res)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}
	res.proContent = proContent

	proSummaryID, err := ids.ToID(hashing.ComputeHash256(proContent))
	if err != nil {
		return nil, fmt.Errorf("cannot compute summary ID: %w", err)
	}
	res.proSummaryID = proSummaryID
	return res, nil
}

func init() {
	lc := linearcodec.New(reflectcodec.DefaultTagName, math.MaxUint32)
	stateSyncCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&ProposerSummaryContent{}),
		stateSyncCodec.RegisterCodec(StateSummaryVersion, lc),
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
	coreSummary, err := vm.coreStateSyncVM.StateSyncGetLastSummary()
	if err != nil {
		return nil, err
	}

	// retrieve ProBlkID
	proBlkID, err := vm.GetBlockIDAtHeight(coreSummary.Key())
	if err != nil {
		return nil, err
	}

	return newSummary(proBlkID, coreSummary)
}

// Note: it's important that ParseSummary do not use any index or state.
func (vm *VM) ParseSummary(summaryBytes []byte) (common.Summary, error) {
	if vm.coreStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	proContent := ProposerSummaryContent{}
	ver, err := stateSyncCodec.Unmarshal(summaryBytes, &proContent)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal ProposerSummaryContent due to: %w", err)
	}
	if ver != StateSummaryVersion {
		return nil, errWrongStateSyncVersion
	}

	coreSummary, err := vm.coreStateSyncVM.ParseSummary(proContent.CoreContent)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal coreSummaryContent due to: %w", err)
	}

	return newSummary(proContent.ProBlkID, coreSummary)
}

func (vm *VM) StateSyncGetSummary(key uint64) (common.Summary, error) {
	if vm.coreStateSyncVM == nil {
		return nil, common.ErrStateSyncableVMNotImplemented
	}

	coreSummary, err := vm.coreStateSyncVM.StateSyncGetSummary(key)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve core summary due to: %w", err)
	}

	// retrieve ProBlkID
	proBlkID, err := vm.GetBlockIDAtHeight(coreSummary.Key())
	if err != nil {
		// this should never happen, it's proVM being out of sync with coreVM
		vm.ctx.Log.Warn("core summary unknown to proposer VM. Block height index missing")
		return nil, err
	}

	return newSummary(proBlkID, coreSummary)
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
		if ver != StateSummaryVersion {
			return errWrongStateSyncVersion
		}

		coreSummary, err := vm.coreStateSyncVM.ParseSummary(proContent.CoreContent)
		if err != nil {
			return fmt.Errorf("could not parse coreSummaryContent due to: %w", err)
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

	proBlkID, err := vm.GetBlockIDAtHeight(coreSummary.Key())
	if err != nil {
		return nil, err
	}

	return newSummary(proBlkID, coreSummary)
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
