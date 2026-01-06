// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-avalanchego"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	network := tmpnet.LocalNetworkOrPanic()
	if err := antithesis.GenerateComposeConfig(network, baseImageName); err != nil {
		tests.NewDefaultLogger("").Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
}
