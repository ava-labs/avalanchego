// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/antithesis"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const baseImageName = "antithesis-xsvm"

// Creates docker-compose.yml and its associated volumes in the target path.
func main() {
	network := tmpnet.LocalNetworkOrPanic()
	network.Subnets = []*tmpnet.Subnet{
		subnet.NewXSVMOrPanic("xsvm", genesis.VMRQKey, network.Nodes...),
	}

	// Path to the plugin dir on subnet-evm node images that will be run by docker compose.
	// TODO(marun) Make this a reusable constant and avoid having to specify it?
	runtimePluginDir := "/avalanchego/build/plugins"

	if err := antithesis.GenerateComposeConfig(network, baseImageName, runtimePluginDir); err != nil {
		tests.NewDefaultLogger("").Fatal("failed to generate compose config",
			zap.Error(err),
		)
		os.Exit(1)
	}
}
