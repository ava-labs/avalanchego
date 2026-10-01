// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extrastest

import (
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
)

func ForkToRules(fork upgradetest.Fork) *extras.Rules {
	chainConfig, ok := paramstest.ForkToChainConfig[fork]
	if !ok {
		panic(fmt.Sprintf("unknown fork: %s", fork))
	}
	return params.GetRulesExtra(chainConfig.Rules(common.Big0, params.IsMergeTODO, 0))
}
