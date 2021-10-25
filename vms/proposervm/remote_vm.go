package proposervm

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	statelessblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func (vm *VM) BatchedParseBlock(blks [][]byte) ([]snowman.Block, error) {
	// Try and batch ParseBlock requests (it reduces network overhead)
	if rVM, ok := vm.ChainVM.(block.RemoteVM); ok {
		innerBlks := make([][]byte, 0, len(blks))
		for _, outerBlkBytes := range blks {
			var innerBlkBytes []byte
			if statelessBlock, err := statelessblock.Parse(outerBlkBytes); err == nil {
				innerBlkBytes = statelessBlock.Block() // Retrieve core bytes
			} else {
				// assume it is a preForkBlock and defer parsing to VM
				innerBlkBytes = outerBlkBytes
			}
			innerBlks = append(innerBlks, innerBlkBytes)
		}
		return rVM.BatchedParseBlock(innerBlks)
	}

	// RemoteVM does not apply, try local logic
	res := make([]snowman.Block, 0, len(blks))
	for _, blkBytes := range blks {
		parsedBlk, err := vm.ParseBlock(blkBytes)
		if err != nil {
			return res, err
		}
		res = append(res, parsedBlk)
	}

	return res, nil
}
