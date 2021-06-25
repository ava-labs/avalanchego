// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/app/entry"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/version"
)

func main() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])
	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	processConfig, err := config.GetProcessConfig(v)
	if err != nil {
		fmt.Printf("couldn't load process config: %s\n", err)
		os.Exit(1)
	}

	if processConfig.DisplayVersionAndExit {
		fmt.Print(version.String(version.GitCommit))
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v, processConfig.BuildDir)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	entry.Run(processConfig, nodeConfig)
}
