// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	baseImageName = "antithesis-avalanchego"

	// cchainBlockCount is the number of C-chain blocks to generate when
	// seeding the bootstrap database for BlockDB migration testing.
	cchainBlockCount = 500
)

// Creates docker-compose.yml and its associated volumes in the target path.
// The bootstrap node's database is pre-seeded with C-chain blocks written to
// ethdb (with BlockDB disabled) so that nodes run the BlockDB migration path
// on startup.
func main() {
	log := tests.NewDefaultLogger("")
	network := tmpnet.LocalNetworkOrPanic()

	if err := antithesis.GenerateComposeConfig(network, baseImageName, cchainBlockCount); err != nil {
		log.Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
}
