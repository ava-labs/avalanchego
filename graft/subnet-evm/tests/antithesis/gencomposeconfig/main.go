// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests/utils"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-subnet-evm"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	// Create a network with a subnet-evm subnet
	network := tmpnet.LocalNetworkOrPanic()
	network.Subnets = []*tmpnet.Subnet{
		utils.NewTmpnetSubnet("subnet-evm", tests.Genesis, utils.DefaultChainConfig, network.Nodes...),
	}

	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		log.Fatalf("failed to generate compose config: %v", err)
	}
}
