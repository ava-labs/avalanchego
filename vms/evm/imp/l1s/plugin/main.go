// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// The plugin binary serves the SAE-based L1 VM as an out-of-process rpcchainvm
// plugin.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/adaptor"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
)

func main() {
	// Register libevm extras (params, core, customtypes) shared with the
	// legacy `graft/subnet-evm` plugin. MUST run before any chain-config
	// unmarshal. `params.RegisterExtras()` is process-global and panics
	// on double-registration; this binary is the sole owner.
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()

	printVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *printVersion {
		fmt.Printf(
			"L1/%s [rpcchainvm=%d]\n",
			version.Current.SemanticWithCommit(version.GitCommit),
			version.RPCChainVMProtocol,
		)
		os.Exit(0)
	}

	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		log.Error("setting fd limit", "error", err)
		os.Exit(1)
	}
	if err := rpcchainvm.Serve(context.Background(), adaptor.Convert(l1s.New())); err != nil {
		log.Error("serving rpc chain vm", "error", err)
		os.Exit(1)
	}
}
