// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/version"

	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/plugin/runner"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	versionString := fmt.Sprintf("Subnet-EVM/%s [AvalancheGo=%s, rpcchainvm=%d]", evm.Version, version.Current, version.RPCChainVMProtocol)
	runner.Run(versionString)
}
