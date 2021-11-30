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

func (vm *VM) StateSyncGetLastSummary() ([]byte, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return nil, block.ErrStateSyncableVMNotImplemented
	}
	// TODO: If this has a race condition with blocks being accepted
	// between time the ChainVM gets the state summary, we can either
	// (a) return (height uint64, summary []byte, error) from this method
	// (b) encode the height or inner blk id in the summary bytes.
	// (c) ? other solutions may be possible too

	// find last syncable height
	lastAcceptedID, err := vm.GetLastAccepted()
	if err != nil {
		return nil, err
	}

	block, err := vm.GetBlock(lastAcceptedID)
	if err != nil {
		return nil, err
	}
	height := block.Height()
	syncableHeight := height - height%4096
	// TODO: we can cache the result here and also
	// proactively set it on processing blocks.
	for ; height > syncableHeight; height-- {
		block, err = vm.GetBlock(block.Parent())
		if err != nil {
			return nil, err
		}
	}
	vmSummary, err := fsVM.StateSyncGetLastSummary()
	if err != nil {
		return nil, err
	}
	blockBytes := block.Bytes()
	packerLen := len(blockBytes) + wrappers.IntLen + len(vmSummary)
	packer := wrappers.Packer{Bytes: make([]byte, packerLen)}
	packer.PackBytes(blockBytes)
	packer.PackFixedBytes(vmSummary)
	return packer.Bytes, nil
}

func (vm *VM) StateSyncIsSummaryAccepted(summary []byte) (bool, error) {
	fsVM, ok := vm.ChainVM.(block.StateSyncableVM)
	if !ok {
		return false, block.ErrStateSyncableVMNotImplemented
	}

	packer := wrappers.Packer{Bytes: summary}
	blockBytes := packer.UnpackBytes()
	_, err := vm.ParseBlock(blockBytes)
	if err != nil {
		return false, err
	}
	// TODO: Validate the contents of the block here.

	vmSummary := packer.UnpackFixedBytes(len(summary) - len(blockBytes) - wrappers.IntLen)
	return fsVM.StateSyncIsSummaryAccepted(vmSummary)
}

func (vm *VM) StateSync(accepted [][]byte) error {
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
	var maxHeightSummary []byte
	var maxHeightBlock PostForkBlock
	for _, summary := range accepted {
		packer := wrappers.Packer{Bytes: summary}
		blockBytes := packer.UnpackBytes()
		parsedBlock, err := vm.parsePostForkBlock(blockBytes)
		if err != nil {
			return err
		}
		height := parsedBlock.Height()
		if maxHeight < height {
			maxHeight = height
			maxHeightSummary = packer.UnpackFixedBytes(len(summary) - len(blockBytes) - wrappers.IntLen)
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

	return fsVM.StateSync([][]byte{maxHeightSummary})
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
