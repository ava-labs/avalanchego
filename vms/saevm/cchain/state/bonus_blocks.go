// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/json"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// mainnetBonusBlocks is the set of mainnet bonus block heights. Bonus blocks
// are indexed in the atomic trie but their shared memory operations MUST NOT be
// applied.
var mainnetBonusBlocks = mustParseBonusBlocks()

//go:embed bonus_blocks.json
var bonusBlocksJSON []byte

func mustParseBonusBlocks() map[uint64]struct{} {
	var heights []uint64
	if err := json.Unmarshal(bonusBlocksJSON, &heights); err != nil {
		panic(err)
	}

	set := make(map[uint64]struct{}, len(heights))
	for _, height := range heights {
		set[height] = struct{}{}
	}
	return set
}

func (s *State) isBonus(height uint64) bool {
	if s.snowCtx.NetworkID != constants.MainnetID {
		return false
	}
	_, ok := mainnetBonusBlocks[height]
	return ok
}
