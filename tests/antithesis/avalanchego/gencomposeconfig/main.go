// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"

	"github.com/ava-labs/avalanchego/tests/antithesis"
)

const (
	baseImageName = "antithesis-avalanchego"
	NumNodes      = 6
)

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	network := antithesis.CreateNetwork(NumNodes)
	if err := antithesis.GenerateComposeConfig(network, baseImageName, "" /* runtimePluginDir */); err != nil {
		log.Fatalf("failed to generate compose config: %v", err)
	}
}
