// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

var DefaultChainConfig = map[string]any{
	"log-level":         "debug",
	"warp-api-enabled":  true,
	"local-txs-enabled": true,
}

func NewTmpnetNetwork(owner string, nodes []*tmpnet.Node, flags tmpnet.FlagsMap) *tmpnet.Network {
	defaultFlags := tmpnet.FlagsMap{}
	defaultFlags.SetDefaults(flags)
	defaultFlags.SetDefaults(tmpnet.FlagsMap{
		config.ProposerVMUseCurrentHeightKey: "true",
	})
	return &tmpnet.Network{
		Owner:        owner,
		DefaultFlags: defaultFlags,
		Nodes:        nodes,
	}
}
