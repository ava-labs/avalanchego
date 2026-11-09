// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Plugin serves the L1 VM as an rpcchainvm plugin.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/libevm"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
)

func main() {
	printVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *printVersion {
		fmt.Printf(
			"L1s/%s [rpcchainvm=%d]\n",
			version.Current.SemanticWithCommit(version.GitCommit),
			version.RPCChainVMProtocol,
		)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	libevm.RegisterExtras()

	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		return fmt.Errorf("setting fd limit: %w", err)
	}
	vm, err := (&l1s.Factory{}).New(logging.NoLog{})
	if err != nil {
		return fmt.Errorf("creating VM: %w", err)
	}
	return rpcchainvm.Serve(context.Background(), vm.(block.ChainVM))
}
