// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ava-labs/avalanchego/utils"
)

const Header = `     _____               .__                       .__
    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o
   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,
  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |
  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\
          \/           \/          \/     \/     \/     \/     \/`

type App interface {
	// Start kicks off the application and returns immediately.
	// Start should only be called once.
	Start()

	// Stop notifies the application to exit and returns immediately.
	// Stop should only be called after [Start].
	// It is safe to call Stop multiple times.
	Stop()

	// ExitCode should only be called after [Start] returns. It
	// should block until the application finishes
	ExitCode() int
}

func Run(app App) int {
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
