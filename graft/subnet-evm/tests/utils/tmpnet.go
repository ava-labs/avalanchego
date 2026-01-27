// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

var DefaultChainConfig = map[string]any{
	"log-level":         "debug",
	"warp-api-enabled":  true,
	"local-txs-enabled": true,
}

func NewTmpnetNetwork(owner string, nodes []*tmpnet.Node, flags tmpnet.FlagsMap, subnets ...*tmpnet.Subnet) *tmpnet.Network {
	defaultFlags := tmpnet.FlagsMap{}
	defaultFlags.SetDefaults(flags)
	defaultFlags.SetDefaults(tmpnet.FlagsMap{
		config.ProposerVMUseCurrentHeightKey: "true",
	})
	return &tmpnet.Network{
		Owner:        owner,
		DefaultFlags: defaultFlags,
		Nodes:        nodes,
		Subnets:      subnets,
	}
}

// Create the configuration that will enable creation and access to a
// subnet created on a temporary network.
func NewTmpnetSubnet(name string, genesis []byte, chainConfig map[string]any, nodes ...*tmpnet.Node) *tmpnet.Subnet {
	if len(nodes) == 0 {
		panic("a subnet must be validated by at least one node")
	}

	validatorIDs := make([]ids.NodeID, len(nodes))
	for i, node := range nodes {
		validatorIDs[i] = node.NodeID
	}

	chainConfigBytes, err := json.Marshal(chainConfig)
	if err != nil {
		panic(err)
	}

	return &tmpnet.Subnet{
		Name: name,
		Chains: []*tmpnet.Chain{
			{
				VMID:         evm.ID,
				Genesis:      genesis,
				Config:       string(chainConfigBytes),
				PreFundedKey: tmpnet.HardhatKey,
			},
		},
		ValidatorIDs: validatorIDs,
	}
}
