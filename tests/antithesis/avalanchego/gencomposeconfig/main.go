// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	baseImageName = "antithesis-avalanchego"
	nodeCount     = 5
)

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	log := tests.NewDefaultLogger("")
	network, err := newNetwork()
	if err != nil {
		log.Fatal("failed to configure network",
			zap.Error(err),
		)
		os.Exit(1)
	}
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		log.Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
}

// newNetwork returns a network with all upgrades initially active. A custom
// network is required because nodes refuse to override the local network's
// upgrade schedule.
func newNetwork() (*tmpnet.Network, error) {
	network := &tmpnet.Network{
		Owner: baseImageName,
		Nodes: tmpnet.NewNodesOrPanic(nodeCount),
	}
	genesis, err := network.DefaultGenesis()
	if err != nil {
		return nil, fmt.Errorf("failed to create genesis: %w", err)
	}
	network.Genesis = genesis

	network.DefaultFlags, err = tmpnet.UpgradeFlags(tmpnet.UpgradeConfig(0))
	if err != nil {
		return nil, fmt.Errorf("failed to get upgrade flags: %w", err)
	}
	return network, nil
}
