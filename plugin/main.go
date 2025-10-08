// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/ulimit"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/ava-labs/coreth/plugin/factory"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	version, err := PrintVersion()
	if err != nil {
		fmt.Printf("couldn't get config: %s\n", err)
		os.Exit(1)
	}
	if version {
		fmt.Println(evm.Version)
		os.Exit(0)
	}
	if err := ulimit.Set(ulimit.DefaultFDLimit, logging.NoLog{}); err != nil {
		fmt.Printf("failed to set fd limit correctly due to: %s\n", err)
		os.Exit(1)
	}

	if err := rpcchainvm.Serve(context.Background(), factory.NewPluginVM()); err != nil {
		fmt.Printf("failed to serve rpc chain vm: %s\n", err)
		os.Exit(1)
	}
}
