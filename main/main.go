// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"golang.org/x/term"

	"github.com/ava-labs/avalanchego/app"
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

	if v.GetBool(config.VersionKey) {
		fmt.Print(version.String)
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	nodeApp := app.New(nodeConfig) // Create node wrapper
	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(app.Header)
	}

	exitCode := app.Run(nodeApp)
	os.Exit(exitCode)
}
