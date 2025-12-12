// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/tests/utils"
)

const baseImageName = "antithesis-subnet-evm"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	// Assume the working directory is the root of the repository
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get current working directory: %s", err)
	}

	genesisPath := filepath.Join(cwd, "tests/load/genesis/genesis.json")

	// Create a network with a subnet-evm subnet
	network := tmpnet.LocalNetworkOrPanic()
	network.Subnets = []*tmpnet.Subnet{
		utils.NewTmpnetSubnet("subnet-evm", genesisPath, utils.DefaultChainConfig, network.Nodes...),
	}

	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		log.Fatalf("failed to generate compose config: %v", err)
	}
}
