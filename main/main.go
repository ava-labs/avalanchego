// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ava-labs/coreth/plugin/evm"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils"
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
		fmt.Println(Header)
	}

	runner, err := node.NewRunner(nodeConfig)
	if err != nil {
		fmt.Printf("couldn't start node: %s\n", err)
		os.Exit(1)
	}

	os.Exit(Run(runner))
}

const Header = `     _____               .__                       .__
    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o
   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,
  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |
  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\
          \/           \/          \/     \/     \/     \/     \/`

func Run(app *node.Runner) int {
	// start running the application
	app.Start()

	// register terminationSignals to kill the application
	terminationSignals := make(chan os.Signal, 1)
	signal.Notify(terminationSignals, syscall.SIGINT, syscall.SIGTERM)

	stackTraceSignal := make(chan os.Signal, 1)
	signal.Notify(stackTraceSignal, syscall.SIGABRT)

	// start up a new go routine to handle attempts to kill the application
	go func() {
		for range terminationSignals {
			app.Stop()
			return
		}
	}()

	// start a goroutine to listen on SIGABRT signals,
	// to print the stack trace to standard error.
	go func() {
		for range stackTraceSignal {
			fmt.Fprint(os.Stderr, utils.GetStacktrace(true))
		}
	}()

	// wait for the app to exit and get the exit code response
	exitCode := app.ExitCode()

	// shut down the termination signal go routine
	signal.Stop(terminationSignals)
	close(terminationSignals)

	// shut down the stack trace go routine
	signal.Stop(stackTraceSignal)
	close(stackTraceSignal)

	// return the exit code that the application reported
	return exitCode
}
