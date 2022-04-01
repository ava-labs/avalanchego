// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// SyncableBlock provides the information necessary to sync a node starting
// at the given block.
type SyncableBlock struct {
	BlockNumber uint64      `serialize:"true"`
	BlockRoot   common.Hash `serialize:"true"`
	AtomicRoot  common.Hash `serialize:"true"`
	BlockHash   common.Hash `serialize:"true"`
}

func (s SyncableBlock) String() string {
	return fmt.Sprintf("SyncableBlock(BlockHash=%s, BlockNumber=%d, BlockRoot=%s, AtomicRoot=%s)", s.BlockHash, s.BlockNumber, s.BlockRoot, s.AtomicRoot)
}
