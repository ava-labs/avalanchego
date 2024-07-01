// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"
	"os"

	"github.com/google/uuid"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func main() {
	// Need to accept parameters for
	// - AvalancheGoPath
	// - NetworkID
	// - StateSyncEnabled

	// Create a new single-node network that will bootstrap
	network := &tmpnet.Network{
		UUID:         uuid.NewString(),
		NetworkID:    constants.TestnetID,
		Owner:        "bootstrap-test",
		Nodes:        tmpnet.NewNodesOrPanic(1),
		DefaultFlags: tmpnet.DefaultLocalhostFlags,
		DefaultRuntimeConfig: tmpnet.NodeRuntimeConfig{
			AvalancheGoPath: "/home/maru/src/avalanchego/master/build/avalanchego",
		},
		ChainConfigs: map[string]tmpnet.FlagsMap{
			"C": {
				"state-sync-enabled": true,
			},
		},
	}

	err := network.Create("")
	if err != nil {
		log.Fatalf("Failed to create network: %v\n", err)
		return
	}

	err = network.StartNodes(context.Background(), os.Stdout, network.Nodes...)
	if err != nil {
		log.Fatalf("Failed to start node: %v\n", err)
		return
	}
}
