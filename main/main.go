// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/ava-labs/avalanchego/app/runner"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/version"
)

func main() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])

	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}

	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	runnerConfig := config.GetRunnerConfig(v)
	if runnerConfig.DisplayVersionAndExit {
		fmt.Print(version.String)
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	runner.Run(runnerConfig, nodeConfig)
}
