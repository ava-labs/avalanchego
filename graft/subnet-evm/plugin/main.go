// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/plugin/evm"
	"github.com/MetalBlockchain/metalgo/graft/subnet-evm/plugin/runner"
	"github.com/MetalBlockchain/metalgo/version"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	versionString := fmt.Sprintf("Subnet-EVM/%s [rpcchainvm=%d]", version.Current.SemanticWithCommit(version.GitCommit), version.RPCChainVMProtocol)
	runner.Run(versionString)
}
