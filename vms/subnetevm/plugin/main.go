// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"

	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/subnetevm/plugin/runner"

	legacyevm "github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
)

func main() {
	// Register libevm extras (params, core, customtypes) shared with the
	// legacy `graft/subnet-evm` plugin. MUST run before any chain-config
	// unmarshal. `params.RegisterExtras()` is process-global and panics
	// on double-registration; this binary is the sole owner.
	legacyevm.RegisterAllLibEVMExtras()

	versionString := fmt.Sprintf(
		"Subnet-EVM-SAE/%s [rpcchainvm=%d]",
		version.Current.SemanticWithCommit(version.GitCommit),
		version.RPCChainVMProtocol,
	)
	runner.Run(versionString)
}
