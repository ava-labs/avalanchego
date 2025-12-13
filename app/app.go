// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
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

// Header is a standard ASCII art display for the application startup.
const Header = `      _____            __                .__
    /  _  \___ _______ |  | _____    ____  |  |__    ____  
   /  /_\  \  \/ /\__ \ |  | \__ \  /    \_/ ___\|  |  \_/ __ \  
  /    |    \    /  / __ \|  |__/ __ \|  |  \  \___|  |   \  ___/
  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >
          \/          \/          \/     \/  	\/     \/  	\/`

var _ App = (*app)(nil)

// App defines the interface for the main application lifecycle management.
type App interface {
	// Start begins the application's main logic (the node) and returns immediately.
	Start()

	// Stop signals the application to gracefully shut down.
	// It's safe to call Stop multiple times.
	Stop()

	// ExitCode blocks until the application finishes and returns the exit status.
	ExitCode() int
}

// New initializes the application wrapper around a node.
func New(config nodeconfig.Config) (App, error) {
	// 1. Set required permissions on data directories.
	if err := perms.ChmodR(config.DatabaseConfig.Path, true, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to restrict permissions of the database directory: %w", err)
	}
	if err := perms.ChmodR(config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("failed to restrict permissions of the log directory: %w", err)
	}

	logFactory := logging.NewFactory(config.LoggingConfig)
	// Use defer to ensure factory resources are closed on initialization failure.
	defer func() {
		if r := recover(); r != nil {
			logFactory.Close()
			panic(r)
		}
	}()

	log, err := logFactory.Make("main")
	if err != nil {
		logFactory.Close() // Explicitly close if log initialization fails
		return nil, fmt.Errorf("failed to initialize log: %w", err)
	}

	// 2. Update file descriptor limit.
	fdLimit := config.FdLimit
	if err := ulimit.Set(fdLimit, log); err != nil {
		// Log the failure, but return the error to the caller for graceful exit
		log.Error("failed to set fd-limit", zap.Error(err))
		logFactory.Close()
		return nil, err
	}

	// 3. Initialize the core node.
	n, err := node.New(&config, logFactory, log)
	if err != nil {
		// Log.Stop() is implicitly called by the defer in Start on error, but good practice to close resources.
		log.Error("failed to initialize node", zap.Error(err))
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

// Run executes the application's main loop, handling system signals.
func Run(app App) int {
	// Start the application's business logic.
	app.Start()

	// Setup channels to catch termination signals (SIGINT, SIGTERM) and stack trace signal (SIGABRT).
	terminationSignals := make(chan os.Signal, 1)
	signal.Notify(terminationSignals, syscall.SIGINT, syscall.SIGTERM)

	stackTraceSignal := make(chan os.Signal, 1)
	signal.Notify(stackTraceSignal, syscall.SIGABRT)

	// Goroutine to handle termination signals: calls Stop() on the application.
	go func() {
		for range terminationSignals {
			app.Stop()
			return
		}
	}()

	// Goroutine to handle SIGABRT: prints the current stack trace to stderr.
	go func() {
		for range stackTraceSignal {
			fmt.Fprint(os.Stderr, utils.GetStacktrace(true))
		}
	}()

	// Block and wait for the application to exit gracefully, then retrieve the exit code.
	exitCode := app.ExitCode()

	// Clean up signal handlers and channels.
	signal.Stop(terminationSignals)
	close(terminationSignals)
	signal.Stop(stackTraceSignal)
	close(stackTraceSignal)

	return exitCode
}

// app is a process wrapper managing the lifecycle of an AvalancheGo node.
type app struct {
	node       *node.Node
	log        logging.Logger
	logFactory logging.Factory
	exitWG     sync.WaitGroup // Waits for the internal node goroutine to finish.
}

// Start kicks off the node's main dispatch loop in a new goroutine.
func (a *app) Start() {
	// ExitCode() will block until a.exitWG.Done() is called.
	a.exitWG.Add(1)

	go func() {
		// Outer defer: executes after the inner defer/panic, cleans up resources.
		defer func() {
			if r := recover(); r != nil {
				// Log the panic that occurred outside of the node's dispatch.
				fmt.Println("caught panic", r)
			}
			a.log.Stop()
			a.logFactory.Close()
			a.exitWG.Done()
		}()

		// Inner defer: executes first on panic, calls StopOnPanic to ensure logs are flushed.
		defer func() {
			a.log.StopOnPanic()
		}()

		// Start the node's main event loop.
		err := a.node.Dispatch()
		a.log.Debug("dispatch returned",
			zap.Error(err),
		)
	}()
}

// Stop attempts to gracefully shut down the running node.
func (a *app) Stop() {
	// The shutdown timeout is set to 0, which typically means immediate/default shutdown.
	a.node.Shutdown(0)
}

// ExitCode blocks until the application's main goroutine (Dispatch) has finished
// and returns the node's reported exit status.
func (a *app) ExitCode() int {
	a.exitWG.Wait()
	return a.node.ExitCode()
}
