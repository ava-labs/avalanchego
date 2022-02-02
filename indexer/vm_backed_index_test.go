// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/stretchr/testify/assert"
)

type extendedVM struct {
	*block.TestVM
	*block.TestHeightIndexedVM
}

func TestVMBackedBlockIndex(t *testing.T) {
	// Setup
	pageSize := uint64(64)
	assert := assert.New(t)

	vm := extendedVM{
		TestVM:              &block.TestVM{},
		TestHeightIndexedVM: &block.TestHeightIndexedVM{},
	}
	_, err := newVMBackedBlockIndex(&block.TestVM{})
	assert.Error(err, "non HeightIndexedVM should not be used for the index")

	vm.VerifyHeightIndexF = func() error { return nil }
	vmBackedIndex, err := newVMBackedBlockIndex(vm)
	assert.NoError(err)

	// Populate blocks with random IDs/bytes
	blksList := make([]*snowman.TestBlock, 0)
	blks := make(map[ids.ID]*snowman.TestBlock)
	for height := uint64(0); height < 2*pageSize; height++ {
		blk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			HeightV:    height,
			TimestampV: time.Now(),
			BytesV:     utils.RandomBytes(len(ids.Empty)),
		}

		blksList = append(blksList, blk)
		blks[blk.ID()] = blk
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, ok := blks[blkID]
		if !ok {
			return nil, database.ErrNotFound
		}
		return blk, nil
	}
	vm.GetBlockIDAtHeightF = func(height uint64) (ids.ID, error) {
		if height >= uint64(len(blksList)) {
			return ids.Empty, database.ErrNotFound
		}
		return blksList[height].ID(), nil
	}
	var lastAcceptedBlkID ids.ID
	vm.LastAcceptedF = func() (ids.ID, error) { return lastAcceptedBlkID, nil }

	// Register each block in the VM and after each, make assertions
	for _, blk := range blksList {
		blkID := blk.ID()
		lastAcceptedBlkID = blkID

		lastAcceptedIndex, err := vmBackedIndex.GetLastAccepted()
		assert.NoError(err)
		assert.EqualValues(lastAcceptedIndex.ID, blk.ID())
		assert.EqualValues(lastAcceptedIndex.Bytes, blk.Bytes())

		gotContainer, err := vmBackedIndex.GetContainerByID(blkID)
		assert.NoError(err)
		assert.Equal(blk.Bytes(), gotContainer.Bytes)

		gotIndex, err := vmBackedIndex.GetIndex(blkID)
		assert.NoError(err)
		assert.EqualValues(gotIndex, blk.Height())

		gotContainer, err = vmBackedIndex.GetContainerByIndex(blk.Height())
		assert.NoError(err)
		assert.Equal(gotContainer.Bytes, blk.Bytes())

		containers, err := vmBackedIndex.GetContainerRange(blk.Height(), 1)
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.Equal(blk.Bytes(), containers[0].Bytes)

		containers, err = vmBackedIndex.GetContainerRange(blk.Height(), 2)
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.Equal(blk.Bytes(), containers[0].Bytes)
	}

	// Create a new index with the same VM and ensure contents still there
	newVMBackedIndex, err := newVMBackedBlockIndex(vm)
	assert.NoError(err)

	// Get all of the containers
	containersList, err := newVMBackedIndex.GetContainerRange(0, pageSize)
	assert.NoError(err)
	assert.Len(containersList, int(pageSize))
	containersList2, err := newVMBackedIndex.GetContainerRange(pageSize, pageSize)
	assert.NoError(err)
	assert.Len(containersList2, int(pageSize))
	containersList = append(containersList, containersList2...)

	// Ensure that the data is correct
	sawContainers := ids.Set{}
	for _, container := range containersList {
		assert.False(sawContainers.Contains(container.ID)) // Should only see this container once
		assert.Contains(blks, container.ID)
		assert.EqualValues(blks[container.ID].ID(), container.ID)
	}
}

func TestVMBackedBlockIndexGetContainerByRangeMaxPageSize(t *testing.T) {
	// Setup
	assert := assert.New(t)

	vm := extendedVM{
		TestVM:              &block.TestVM{},
		TestHeightIndexedVM: &block.TestHeightIndexedVM{},
	}
	_, err := newVMBackedBlockIndex(&block.TestVM{})
	assert.Error(err, "non HeightIndexedVM should not be used for the index")

	vm.VerifyHeightIndexF = func() error { return nil }
	vmBackedIndex, err := newVMBackedBlockIndex(vm)
	assert.NoError(err)

	// Insert [MaxFetchedByRange] + 1 containers
	blksList := make([]*snowman.TestBlock, 0)
	blks := make(map[ids.ID]*snowman.TestBlock)
	for height := uint64(0); height < MaxFetchedByRange+1; height++ {
		blk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Accepted,
			},
			HeightV:    height,
			TimestampV: time.Now(),
			BytesV:     utils.RandomBytes(len(ids.Empty)),
		}

		blksList = append(blksList, blk)
		blks[blk.ID()] = blk
	}

	vm.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
		blk, ok := blks[blkID]
		if !ok {
			return nil, database.ErrNotFound
		}
		return blk, nil
	}
	vm.GetBlockIDAtHeightF = func(height uint64) (ids.ID, error) {
		if height >= uint64(len(blksList)) {
			return ids.Empty, database.ErrNotFound
		}
		return blksList[height].ID(), nil
	}
	vm.LastAcceptedF = func() (ids.ID, error) {
		return blksList[len(blksList)-1].ID(), nil
	}

	// Page size too large
	_, err = vmBackedIndex.GetContainerRange(0, MaxFetchedByRange+1)
	assert.Error(err)

	// Make sure data is right
	containers, err := vmBackedIndex.GetContainerRange(0, MaxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers, MaxFetchedByRange)

	containers2, err := vmBackedIndex.GetContainerRange(1, MaxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers2, MaxFetchedByRange)

	assert.Equal(containers[1], containers2[0])
	assert.Equal(containers[MaxFetchedByRange-1], containers2[MaxFetchedByRange-2])

	// Should have last 2 elements
	containers, err = vmBackedIndex.GetContainerRange(MaxFetchedByRange-1, MaxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers, 2)
	assert.EqualValues(containers[1], containers2[MaxFetchedByRange-1])
	assert.EqualValues(containers[0], containers2[MaxFetchedByRange-2])
}
