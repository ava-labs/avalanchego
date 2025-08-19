package node

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/ulimit"

	nodeconfig "github.com/ava-labs/avalanchego/config/node"
)

// Runner is a wrapper around a *Node that runs in this process
type Runner struct {
	node       *Node
	log        logging.Logger
	logFactory logging.Factory
	exitWG     sync.WaitGroup
}

func NewRunner(config nodeconfig.Config) (*Runner, error) {
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

	n, err := New(&config, logFactory, log)
	if err != nil {
		log.Fatal("failed to initialize node", zap.Error(err))
		log.Stop()
		logFactory.Close()
		return nil, fmt.Errorf("failed to initialize node: %w", err)
	}

	return &Runner{
		node:       n,
		log:        log,
		logFactory: logFactory,
	}, nil
}

// Start the business logic of the node (as opposed to config reading, etc).
// Does not block until the node is done.
func (a *Runner) Start() {
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

		err := a.node.Dispatch()
		a.log.Debug("dispatch returned",
			zap.Error(err),
		)
	}()
}

// Stop attempts to shutdown the currently running node. This function will
// block until Shutdown returns.
func (a *Runner) Stop() {
	a.node.Shutdown(0)
}

// ExitCode returns the exit code that the node is reporting. This function
// blocks until the node has been shut down.
func (a *Runner) ExitCode() int {
	a.exitWG.Wait()
	return a.node.ExitCode()
}
