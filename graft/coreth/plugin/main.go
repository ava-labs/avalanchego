// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/MetalBlockchain/metalgo/graft/coreth/plugin/evm"
	"github.com/MetalBlockchain/metalgo/graft/coreth/plugin/factory"
	"github.com/MetalBlockchain/metalgo/utils/logging"
	"github.com/MetalBlockchain/metalgo/utils/ulimit"
	"github.com/MetalBlockchain/metalgo/version"
	"github.com/MetalBlockchain/metalgo/vms/rpcchainvm"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	printVersion, err := PrintVersion()
	if err != nil {
		fmt.Printf("couldn't get config: %s\n", err)
		os.Exit(1)
	}
	if printVersion {
		fmt.Println(version.Current.SemanticWithCommit(version.GitCommit))
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
