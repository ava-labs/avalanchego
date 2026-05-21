// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/MetalBlockchain/metalgo/snow/consensus/snowball"
	"github.com/MetalBlockchain/metalgo/snow/consensus/snowman"
	"github.com/MetalBlockchain/metalgo/snow/engine/common/tracker"
	"github.com/MetalBlockchain/metalgo/snow/engine/enginetest"
	"github.com/MetalBlockchain/metalgo/snow/engine/snowman/block/blocktest"
	"github.com/MetalBlockchain/metalgo/snow/snowtest"
	"github.com/MetalBlockchain/metalgo/snow/validators"
)

func DefaultConfig(t testing.TB) Config {
	ctx := snowtest.Context(t, snowtest.PChainID)

	return Config{
		Ctx:                 snowtest.ConsensusContext(ctx),
		VM:                  &blocktest.VM{},
		Sender:              &enginetest.Sender{},
		Validators:          validators.NewManager(),
		ConnectedValidators: tracker.NewPeers(),
		Params: snowball.Parameters{
			K:                     1,
			AlphaPreference:       1,
			AlphaConfidence:       1,
			Beta:                  1,
			ConcurrentRepolls:     1,
			OptimalProcessing:     100,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: &snowman.Topological{Factory: snowball.SnowflakeFactory},
	}
}
