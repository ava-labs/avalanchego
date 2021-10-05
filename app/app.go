// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

type App interface {
	// Start kicks off the application and returns immediately
	Start() error

	// Stop notifies the application to exit and returns immediately
	Stop() error

	// ExitCode should only be called after [Start] returns with no error. It
	// should block until the application finishes
	ExitCode() (int, error)
}

func Run(app App) int {
	// starting running the application
	if err := app.Start(); err != nil {
		return 1
	}

	// register signals to kill the application
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT)
	signal.Notify(signals, syscall.SIGTERM)

	// start up a new go routine to handle attempts to kill the application
	var (
		stopErr error
		wg      sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range signals {
			if err := app.Stop(); err != nil {
				stopErr = err
			}
		}
	}()

	// wait for the app to exit and get the exit code response
	exitCode, err := app.ExitCode()

	// shut down the signal go routine
	signal.Stop(signals)
	close(signals)
	wg.Wait()

	// if there was an error closing the application, report that error
	if stopErr != nil {
		return 1
	}

	// if there was an error running the application, report that error
	if err != nil {
		return 1
	}

	// return the exit code that the application reported
	return exitCode
}
