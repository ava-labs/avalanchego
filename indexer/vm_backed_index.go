// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	_ Index = &vmBackedBlockIndex{}

	errIndexNotReady = errors.New("vm has not yet done indexing containers")
)

type vmBackedBlockIndex struct {
	codec codec.Manager

	vm  block.ChainVM
	hVM block.HeightIndexedChainVM
}

func newVmBackedBlockIndex(codec codec.Manager, vm block.ChainVM) (Index, error) {
	hVM, ok := vm.(block.HeightIndexedChainVM)
	if !ok {
		return nil, block.ErrHeightIndexedVMNotImplemented
	}
	return &vmBackedBlockIndex{
		codec: codec,
		vm:    vm,
		hVM:   hVM,
	}, nil
}

func (vmi *vmBackedBlockIndex) Accept(ctx *snow.ConsensusContext, containerID ids.ID, container []byte) error {
	// nothing to do here. Blocks are retrieved directly from VM.
	return nil
}

func (vmi *vmBackedBlockIndex) getContainerByIndex(index uint64) ([]byte, error) {
	if !vmi.hVM.IsHeightIndexComplete() {
		return nil, errIndexNotReady
	}

	blkID, err := vmi.hVM.GetBlockIDByHeight(index)
	if err != nil {
		return nil, fmt.Errorf("no container at index %d", index)
	}
	blk, err := vmi.vm.GetBlock(blkID)
	if err != nil {
		return nil, fmt.Errorf("no container at index %d", index)
	}

	return blk.Bytes(), nil
}

func (vmi *vmBackedBlockIndex) bytesToContainer(bytes []byte) (Container, error) {
	var container Container
	if _, err := vmi.codec.Unmarshal(bytes, &container); err != nil {
		return Container{}, fmt.Errorf("couldn't unmarshal container: %w", err)
	}
	return container, nil
}

func (vmi *vmBackedBlockIndex) GetContainerByIndex(index uint64) (Container, error) {
	containerBytes, err := vmi.getContainerByIndex(index)
	if err != nil {
		return Container{}, err
	}

	return vmi.bytesToContainer(containerBytes)
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
		return nil, fmt.Errorf("could not retrieve last accepted container")
	}

	lastAcceptedIndex := lastBlk.Height()
	if startIndex > lastBlk.Height() {
		return nil, fmt.Errorf("start index (%d) > last accepted index (%d)", startIndex, lastAcceptedIndex)
	}

	// Calculate the last index we will fetch
	lastIndex := math.Min64(startIndex+numToFetch-1, lastAcceptedIndex)
	// [lastIndex] is always >= [startIndex] so this is safe.
	// [numToFetch] is limited to [MaxFetchedByRange] so [containers] is bounded in size.
	containers := make([]Container, int(lastIndex)-int(startIndex)+1)

	n := 0
	for j := startIndex; j <= lastIndex; j++ {
		containerBytes, err := vmi.getContainerByIndex(j)
		if err != nil {
			return nil, fmt.Errorf("couldn't get container at index %d: %w", j, err)
		}
		containers[n], err = vmi.bytesToContainer(containerBytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't get container at index %d: %w", j, err)
		}
		n++
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
		return Container{}, fmt.Errorf("could not retrieve last accepted container")
	}

	return vmi.bytesToContainer(lastBlk.Bytes())
}

func (vmi *vmBackedBlockIndex) GetIndex(containerID ids.ID) (uint64, error) {
	blk, err := vmi.vm.GetBlock(containerID)
	if err != nil {
		return 0, fmt.Errorf("could not retrieve container %s", containerID)
	}

	return blk.Height(), nil
}

func (vmi *vmBackedBlockIndex) GetContainerByID(containerID ids.ID) (Container, error) {
	blk, err := vmi.vm.GetBlock(containerID)
	if err != nil {
		return Container{}, fmt.Errorf("could not retrieve container %s", containerID)
	}

	return vmi.bytesToContainer(blk.Bytes())
}

func (vmi *vmBackedBlockIndex) Close() error {
	// nothing to do here. Blocks are handled directly in the VM.
	return nil
}
