// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"github.com/ava-labs/avalanchego/tests/reexecute/c/cli/cmd"
	"github.com/ava-labs/coreth/plugin/evm"
)

func main() {
	evm.RegisterAllLibEVMExtras()
	cmd.Execute()
}
