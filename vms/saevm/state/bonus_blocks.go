// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"

	_ "embed"
)

var (
	//go:embed bonus_blocks.json
	bonusBlocksJSON []byte
	bonusBlocks     set.Set[uint64]
)

func init() {
	if err := json.Unmarshal(bonusBlocksJSON, &bonusBlocks); err != nil {
		panic(err)
	}
}

// BonusBlocks returns the collection of bonus blocks for the provided network
// ID. In bonus blocks, shared memory is not applied after executing
// transactions; allowing the same UTXOs to be consumed in multiple blocks.
func BonusBlocks(networkID uint32) set.Set[uint64] {
	if networkID != constants.MainnetID {
		return nil
	}
	return bonusBlocks
}
