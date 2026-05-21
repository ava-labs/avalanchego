// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import "github.com/MetalBlockchain/metalgo/ids"

// readMainnetBonusBlocks returns maps of bonus block numbers to block IDs.
// Note bonus blocks are indexed in the atomic trie.
func readMainnetBonusBlocks() (map[uint64]ids.ID, error) {
	mainnetBonusBlocks := map[uint64]string{}

	bonusBlockMainnetHeights := make(map[uint64]ids.ID)
	for height, blkIDStr := range mainnetBonusBlocks {
		blkID, err := ids.FromString(blkIDStr)
		if err != nil {
			return nil, err
		}
		bonusBlockMainnetHeights[height] = blkID
	}
	return bonusBlockMainnetHeights, nil
}
