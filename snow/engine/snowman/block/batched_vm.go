// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var ErrRemoteVMNotImplemented = errors.New("vm does not implement RemoteVM interface")

// BatchedChainVM extends the minimal functionalities exposed by ChainVM for VMs
// communicating over network (gRPC in our case). This allows more efficient
// operations since calls over network can be duly batched
type BatchedChainVM interface {
	GetAncestors(
		blkID ids.ID, // first requested block
		maxBlocksNum int, // max number of blocks to be retrieved
		maxBlocksSize int, // max cumulated byte size of retrieved blocks
		maxBlocksRetrivalTime time.Duration, // max duration of retrival operation
	) ([][]byte, error)

	BatchedParseBlock(blks [][]byte) ([]snowman.Block, error)
}

func GetAncestors(
	vm Getter, // fetch blocks
	blkID ids.ID, // first requested block
	maxBlocksNum int, // max number of blocks to be retrieved
	maxBlocksSize int, // max cumulated byte size of retrieved blocks
	maxBlocksRetrivalTime time.Duration, // max duration of retrival operation
) ([][]byte, error) {
	// Try and batch GetBlock requests
	if vm, ok := vm.(BatchedChainVM); ok {
		blocks, err := vm.GetAncestors(
			blkID,
			maxBlocksNum,
			maxBlocksSize,
			maxBlocksRetrivalTime,
		)
		if err == nil {
			return blocks, nil
		}
		if err != ErrRemoteVMNotImplemented {
			return nil, err
		}
	}

	// RemoteVM did not work, try local logic
	startTime := time.Now()
	blk, err := vm.GetBlock(blkID)
	if err != nil {
		return nil, err
	}

	// First elt is byte repr. of [blk], then its parent, then grandparent, etc.
	ancestorsBytes := make([][]byte, 1, maxBlocksNum)
	ancestorsBytes[0] = blk.Bytes()
	ancestorsBytesLen := len(blk.Bytes()) + wrappers.IntLen // length, in bytes, of all elements of ancestors

	for numFetched := 1; numFetched < maxBlocksNum && time.Since(startTime) < maxBlocksRetrivalTime; numFetched++ {
		if blk, err = vm.GetBlock(blk.Parent()); err != nil {
			break
		}
		blkBytes := blk.Bytes()
		// Ensure response size isn't too large. Include wrappers.IntLen because
		// the size of the message is included with each container, and the size
		// is repr. by an int.
		if newLen := ancestorsBytesLen + len(blkBytes) + wrappers.IntLen; newLen <= maxBlocksSize {
			ancestorsBytes = append(ancestorsBytes, blkBytes)
			ancestorsBytesLen = newLen
		} else { // reached maximum response size
			break
		}
	}

	return ancestorsBytes, nil
}

func BatchedParseBlock(vm Parser, blks [][]byte) ([]snowman.Block, error) {
	// Try and batch ParseBlock requests
	if vm, ok := vm.(BatchedChainVM); ok {
		blocks, err := vm.BatchedParseBlock(blks)
		if err == nil {
			return blocks, nil
		}
		if err != ErrRemoteVMNotImplemented {
			return nil, err
		}
	}

	// We couldn't batch the ParseBlock requests, try to parse them one at a
	// time.
	blocks := make([]snowman.Block, len(blks))
	for i, blockBytes := range blks {
		block, err := vm.ParseBlock(blockBytes)
		if err != nil {
			return nil, err
		}
		blocks[i] = block
	}
	return blocks, nil
}
