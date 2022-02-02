// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ Index = &vmBackedBlockIndex{}

	errIndexNotReady = errors.New("vm has not yet done indexing containers")
)

type vmBackedBlockIndex struct {
	vm  block.ChainVM
	hVM block.HeightIndexedChainVM
}

func newVMBackedBlockIndex(vm common.VM) (Index, error) {
	bVM, ok := vm.(block.ChainVM)
	if !ok {
		return nil, block.ErrHeightIndexedVMNotImplemented
	}
	hVM, ok := bVM.(block.HeightIndexedChainVM)
	if !ok {
		return nil, block.ErrHeightIndexedVMNotImplemented
	}
	if hVM.VerifyHeightIndex() != nil {
		return nil, errIndexNotReady
	}

	return &vmBackedBlockIndex{
		vm:  bVM,
		hVM: hVM,
	}, nil
}

func (vmi *vmBackedBlockIndex) Accept(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error {
	// nothing to do here. Blocks are retrieved directly from VM.
	return nil
}

func (vmi *vmBackedBlockIndex) getBlkByIndex(index uint64) (snowman.Block, error) {
	blkID, err := vmi.hVM.GetBlockIDAtHeight(index)
	if err != nil {
		return nil, fmt.Errorf("no container at index %d: %w", index, err)
	}
	return vmi.vm.GetBlock(blkID)
}

func (vmi *vmBackedBlockIndex) GetContainerByIndex(index uint64) (Container, error) {
	blk, err := vmi.getBlkByIndex(index)
	if err != nil {
		return Container{}, err
	}

	return Container{
		ID:        blk.ID(),
		Bytes:     blk.Bytes(),
		Timestamp: blk.Timestamp().Unix(),
	}, nil
}

func (vmi *vmBackedBlockIndex) GetContainerRange(startIndex uint64, numToFetch uint64) ([]Container, error) {
	// Check arguments for validity
	if numToFetch == 0 {
		return nil, errNumToFetchZero
	} else if numToFetch > MaxFetchedByRange {
		return nil, fmt.Errorf("requested %d but maximum page size is %d", numToFetch, MaxFetchedByRange)
	}

	lastBlkID, err := vmi.vm.LastAccepted()
	if err != nil {
		return nil, errNoneAccepted
	}
	lastBlk, err := vmi.vm.GetBlock(lastBlkID)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve last accepted container: %w", err)
	}

	lastAcceptedIndex := lastBlk.Height()
	if startIndex > lastBlk.Height() {
		return nil, fmt.Errorf("start index (%d) > last accepted index (%d)", startIndex, lastAcceptedIndex)
	}

	// Calculate the last index we will fetch
	lastIndex := math.Min64(startIndex+numToFetch-1, lastAcceptedIndex)
	// [lastIndex] is always >= [startIndex] so this is safe.
	// [numToFetch] is limited to [MaxFetchedByRange] so [containers] is bounded in size.
	containers := make([]Container, 0, int(lastIndex)-int(startIndex)+1)

	for j := startIndex; j <= lastIndex; j++ {
		blk, err := vmi.getBlkByIndex(j)
		if err != nil {
			return nil, fmt.Errorf("couldn't get container at index %d: %w", j, err)
		}
		containers = append(containers, Container{
			ID:        blk.ID(),
			Bytes:     blk.Bytes(),
			Timestamp: blk.Timestamp().Unix(),
		})
	}
	return containers, nil
}

func (vmi *vmBackedBlockIndex) GetLastAccepted() (Container, error) {
	lastBlkID, err := vmi.vm.LastAccepted()
	if err != nil {
		return Container{}, errNoneAccepted
	}
	lastBlk, err := vmi.vm.GetBlock(lastBlkID)
	if err != nil {
		return Container{}, fmt.Errorf("could not retrieve last accepted container: %w", err)
	}

	return Container{
		ID:        lastBlk.ID(),
		Bytes:     lastBlk.Bytes(),
		Timestamp: lastBlk.Timestamp().Unix(),
	}, nil
}

func (vmi *vmBackedBlockIndex) GetIndex(containerID ids.ID) (uint64, error) {
	blk, err := vmi.vm.GetBlock(containerID)
	if err != nil {
		return 0, fmt.Errorf("could not retrieve container %s: %w", containerID, err)
	}

	return blk.Height(), nil
}

func (vmi *vmBackedBlockIndex) GetContainerByID(containerID ids.ID) (Container, error) {
	blk, err := vmi.vm.GetBlock(containerID)
	if err != nil {
		return Container{}, fmt.Errorf("could not retrieve container %s: %w", containerID, err)
	}

	return Container{
		ID:        blk.ID(),
		Bytes:     blk.Bytes(),
		Timestamp: blk.Timestamp().Unix(),
	}, nil
}

func (vmi *vmBackedBlockIndex) Close() error {
	// nothing to do here. Blocks are handled directly in the VM.
	return nil
}
