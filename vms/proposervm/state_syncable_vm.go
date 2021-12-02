// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

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

	// TODO ABENEGIA:
	// 1- Find proBlkID corresponding to vmSummary; note that vmSummary.Key == innerBlkID + hash
	// 2- Return summary whose key is proBlkID + innerBlkID + hash
	// Enabler: add mapping from innerBlkID to proBlkID. It should be just for accepted blocks, hence it's bijective map
	return vmSummary, err
}

func (vm *VM) StateSyncIsSummaryAccepted(key []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	packer := wrappers.Packer{Bytes: key}
	blockBytes := packer.UnpackBytes()
	_, err := vm.ParseBlock(blockBytes)
	if err != nil {
		return false, err
	}
	// TODO: Validate the contents of the block here.

	vmKeys := packer.UnpackFixedBytes(len(key) - len(blockBytes) - wrappers.IntLen)
	return fsVM.StateSyncIsSummaryAccepted(vmKeys)
}

func (vm *VM) StateSync(accepted []block.Summary) error {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return block.ErrStateSyncableVMNotImplemented
	}
	if len(accepted) == 0 {
		return fsVM.StateSync(accepted)
	}

	// unwrap the expected block id, height, and vmSummary data
	// then pass the vmSummary corresponding to the largest height
	// to the fsVM.
	maxHeight := uint64(0)
	var maxKey []byte
	var maxHeightState []byte
	var maxHeightBlock PostForkBlock
	for _, summary := range accepted {
		packer := wrappers.Packer{Bytes: summary.State}
		blockBytes := packer.UnpackBytes()
		parsedBlock, err := vm.parsePostForkBlock(blockBytes)
		if err != nil {
			return err
		}
		height := parsedBlock.Height()
		if maxHeight < height {
			maxHeight = height
			maxKey = summary.Key
			maxHeightState = packer.UnpackFixedBytes(len(summary.State) - len(blockBytes) - wrappers.IntLen)
			maxHeightBlock = parsedBlock
		}
	}

	// TODO: This should be done on complete instead.
	// It may also be OK to do this first and once either
	// vm.ChainVM or vm.State move on to a state sync block,
	// operations can only be resumed by continuing a state sync.
	if err := vm.State.PutBlock(maxHeightBlock.getStatelessBlk(), choices.Accepted); err != nil {
		return err
	}
	if err := vm.State.SetLastAccepted(maxHeightBlock.ID()); err != nil {
		return err
	}

	summaryToPush := []block.Summary{
		{
			Key:   maxKey,
			State: maxHeightState,
		},
	}
	return fsVM.StateSync(summaryToPush)
}

func (vm *VM) StateSyncLastAccepted() (ids.ID, uint64, error) {
	lastAccepted, err := vm.State.GetLastAccepted()
	if err != nil {
		return ids.Empty, 0, err
	}
	block, err := vm.getBlock(lastAccepted)
	if err != nil {
		return ids.Empty, 0, err
	}
	return lastAccepted, block.Height(), nil
}
