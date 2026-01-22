// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/runner"
	"github.com/ava-labs/avalanchego/version"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	versionString := version.Current.SemanticWithCommit(version.GitCommit)
	runner.Run(versionString)
}
