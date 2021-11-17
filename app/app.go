// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
)

type App interface {
	// Start kicks off the application and returns immediately.
	// Start should only be called once.
	Start() error

	// Stop notifies the application to exit and returns immediately.
	// Stop should only be called after [Start].
	// It is safe to call Stop multiple times.
	Stop() error

	// ExitCode should only be called after [Start] returns with no error. It
	// should block until the application finishes
	ExitCode() (int, error)
}

func Run(app App) int {
	// start running the application
	if err := app.Start(); err != nil {
		return 1
	}

	// register signals to kill the application
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT)
	signal.Notify(signals, syscall.SIGTERM)

	// start up a new go routine to handle attempts to kill the application
	var eg errgroup.Group
	eg.Go(func() error {
		for range signals {
			return app.Stop()
		}
		return nil
	})

	// wait for the app to exit and get the exit code response
	exitCode, err := app.ExitCode()

	// shut down the signal go routine
	signal.Stop(signals)
	close(signals)

	// if there was an error closing or running the application, report that error
	if eg.Wait() != nil || err != nil {
		return 1
	}

	// return the exit code that the application reported
	return exitCode
}
