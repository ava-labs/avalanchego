// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/json"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	//go:embed bonus_blocks.json
	bonusBlocksJSON []byte

	// bonusBlocks is the set of mainnet block heights which were accepted
	// without applying their shared memory operations. This behavior was
	// canonicalized by Coreth and so MUST be replicated. These blocks only
	// included Import transactions.
	bonusBlocks set.Set[uint64]
)

func init() {
	if err := json.Unmarshal(bonusBlocksJSON, &bonusBlocks); err != nil {
		panic(err)
	}
}

func isBonusBlock(networkID uint32, height uint64) bool {
	return networkID == constants.MainnetID &&
		bonusBlocks.Contains(height)
}
