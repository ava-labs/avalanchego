// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func DefaultConfigs() Config {
	commonCfg := common.DefaultConfigTest()
	return Config{
		Ctx:        commonCfg.Ctx,
		Sender:     commonCfg.Sender,
		Validators: validators.NewManager(),
		VM:         &block.TestVM{},
		Params: snowball.Parameters{
			K:                     1,
			AlphaPreference:       1,
			AlphaConfidence:       1,
			BetaVirtuous:          1,
			BetaRogue:             2,
			ConcurrentRepolls:     1,
			OptimalProcessing:     100,
			MaxOutstandingItems:   1,
			MaxItemProcessingTime: 1,
		},
		Consensus: &snowman.Topological{},
	}
}
