// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/ava-labs/avalanchego/app"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/version"
)

func main() {
	evm.RegisterAllLibEVMExtras()

	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])

	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}

	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	if v.GetBool(config.VersionJSONKey) && v.GetBool(config.VersionKey) {
		fmt.Println("can't print both JSON and human readable versions")
		os.Exit(1)
	}

	if v.GetBool(config.VersionJSONKey) {
		versions := version.GetVersions()
		jsonBytes, err := json.MarshalIndent(versions, "", "  ")
		if err != nil {
			fmt.Printf("couldn't marshal versions: %s\n", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonBytes))
		os.Exit(0)
	}

	if v.GetBool(config.VersionKey) {
		fmt.Println(version.GetVersions().String())
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	if term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Println(app.Header)
	}

	nodeApp, err := app.New(nodeConfig)
	if err != nil {
		fmt.Printf("couldn't start node: %s\n", err)
		os.Exit(1)
	}

	exitCode := app.Run(nodeApp)
	os.Exit(exitCode)
}
