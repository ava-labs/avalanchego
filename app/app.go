// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/ulimit"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
)

const Header = `     _____               .__                       .__
    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o
   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,
  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |
  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\
          \/           \/          \/     \/     \/     \/     \/`

var _ App = (*app)(nil)

type App interface {
	// Start kicks off the application and returns immediately.
	// Start should only be called once.
	Start(ctx context.Context)

	// Stop notifies the application to exit and returns immediately.
	// Stop should only be called after [Start].
	// It is safe to call Stop multiple times.
	Stop()

	// ExitCode should only be called after [Start] returns. It
	// should block until the application finishes
	ExitCode() int
}

func New(ctx context.Context, config nodeconfig.Config) (App, error) {
	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(config.DatabaseConfig.Path, true, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to restrict the permissions of the database directory with: %w", err)
	}
	if err := perms.ChmodR(config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to restrict the permissions of the log directory with: %w", err)
	}

	logFactory := logging.NewFactory(config.LoggingConfig)
	log, err := logFactory.Make("main")
	if err != nil {
		logFactory.Close()
		return nil, fmt.Errorf("failed to initialize log: %w", err)
	}

	// update fd limit
	fdLimit := config.FdLimit
	if err := ulimit.Set(fdLimit, log); err != nil {
		log.Fatal("failed to set fd-limit",
			zap.Error(err),
		)
		logFactory.Close()
		return nil, err
	}

	n, err := node.New(ctx, &config, logFactory, log)
	if err != nil {
		log.Fatal("failed to initialize node", zap.Error(err))
		log.Stop()
		logFactory.Close()
		return nil, fmt.Errorf("failed to initialize node: %w", err)
	}

	return &app{
		node:       n,
		log:        log,
		logFactory: logFactory,
	}, nil
}

func Run(ctx context.Context, app App) int {
	// start running the application
	app.Start(ctx)

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

// app is a wrapper around a node that runs in this process
type app struct {
	node       *node.Node
	log        logging.Logger
	logFactory logging.Factory
	exitWG     sync.WaitGroup
}

// Start the business logic of the node (as opposed to config reading, etc).
// Does not block until the node is done.
func (a *app) Start(ctx context.Context) {
	// [p.ExitCode] will block until [p.exitWG.Done] is called
	a.exitWG.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("caught panic", r)
			}
			a.log.Stop()
			a.logFactory.Close()
			a.exitWG.Done()
		}()
		defer func() {
			// If [p.node.Dispatch()] panics, then we should log the panic and
			// then re-raise the panic. This is why the above defer is broken
			// into two parts.
			a.log.StopOnPanic()
		}()

		err := a.node.Dispatch(ctx)
		a.log.Debug("dispatch returned",
			zap.Error(err),
		)
	}()
}

// Stop attempts to shutdown the currently running node. This function will
// block until Shutdown returns.
func (a *app) Stop() {
	a.node.Shutdown(0)
}

// ExitCode returns the exit code that the node is reporting. This function
// blocks until the node has been shut down.
func (a *app) ExitCode() int {
	a.exitWG.Wait()
	return a.node.ExitCode()
}
