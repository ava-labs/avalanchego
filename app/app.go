// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package app

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"go.uber.org/zap"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/nat"
	gonode "github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/ulimit"

	"github.com/chain4travel/camino-node/node"
)

const (
	Header = `
    __   ____  ___ ___  ____  ____    ___  
   /  ] /    ||   |   ||    ||    \  /   \ 
  /  / |  o  || _   _ | |  | |  _  ||     |
 /  /  |     ||  \_/  | |  | |  |  ||  O  |
/   \_ |  _  ||   |   | |  | |  |  ||     |
\     ||  |  ||   |   | |  | |  |  ||     |
 \____||__|__||___|___||____||__|__| \___/ `
)

var (
	stakingPortName = fmt.Sprintf("%s-staking", constants.AppName)
	httpPortName    = fmt.Sprintf("%s-http", constants.AppName)

	_ App = (*app)(nil)
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

func New(config gonode.Config) App {
	return &app{
		config: config,
		node:   &node.Node{},
	}
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

// app is a wrapper around a node that runs in this process
type app struct {
	config gonode.Config
	node   *node.Node
	exitWG sync.WaitGroup
}

// Start the business logic of the node (as opposed to config reading, etc).
// Does not block until the node is done. Errors returned from this method
// are not logged.
func (a *app) Start() error {
	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(a.config.DatabaseConfig.Path, true, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to restrict the permissions of the database directory with: %w", err)
	}
	if err := perms.ChmodR(a.config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to restrict the permissions of the log directory with: %w", err)
	}

	// we want to create the logger after the plugin has started the app
	logFactory := logging.NewFactory(a.config.LoggingConfig)
	log, err := logFactory.Make("main")
	if err != nil {
		logFactory.Close()
		return err
	}

	// update fd limit
	fdLimit := a.config.FdLimit
	if err := ulimit.Set(fdLimit, log); err != nil {
		log.Fatal("failed to set fd-limit",
			zap.Error(err),
		)
		logFactory.Close()
		return err
	}

	// Track if sybil control is enforced
	if !a.config.EnableStaking {
		log.Warn("sybil control is not enforced",
			zap.String("reason", "staking is disabled"),
		)
	}

	// TODO move this to config
	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if a.config.AttemptedNATTraversal && !a.config.Nat.SupportsNAT() {
		log.Warn("UPnP and NAT-PMP router attach failed, you may not be listening publicly. " +
			"Please confirm the settings in your router")
	}

	if ip := a.config.IPPort.IPPort().IP; ip.IsLoopback() || ip.IsPrivate() {
		log.Warn("P2P IP is private, you will not be publicly discoverable",
			zap.Stringer("ip", ip),
		)
	}

	// An empty host is treated as a wildcard to match all addresses, so it is
	// considered public.
	hostIsPublic := a.config.HTTPHost == ""
	if !hostIsPublic {
		ip, err := ips.Lookup(a.config.HTTPHost)
		if err != nil {
			log.Fatal("failed to lookup HTTP host",
				zap.String("host", a.config.HTTPHost),
				zap.Error(err),
			)
			logFactory.Close()
			return err
		}
		hostIsPublic = !ip.IsLoopback() && !ip.IsPrivate()

		log.Debug("finished HTTP host lookup",
			zap.String("host", a.config.HTTPHost),
			zap.Stringer("ip", ip),
			zap.Bool("isPublic", hostIsPublic),
		)
	}

	mapper := nat.NewPortMapper(log, a.config.Nat)

	// Open staking port we want for NAT traversal to have the external port
	// (config.IP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	if port := a.config.IPPort.IPPort().Port; port != 0 {
		mapper.Map(
			port,
			port,
			stakingPortName,
			a.config.IPPort,
			a.config.IPResolutionFreq,
		)
	}

	// Don't open the HTTP port if the HTTP server is private
	if hostIsPublic {
		log.Warn("HTTP server is binding to a potentially public host. "+
			"You may be vulnerable to a DoS attack if your HTTP port is publicly accessible",
			zap.String("host", a.config.HTTPHost),
		)

		// For NAT traversal we want to route from the external port
		// (config.ExternalHTTPPort) to our internal port (config.HTTPPort).
		if a.config.HTTPPort != 0 {
			mapper.Map(
				a.config.HTTPPort,
				a.config.HTTPPort,
				httpPortName,
				nil,
				a.config.IPResolutionFreq,
			)
		}
	}

	// Regularly update our public IP.
	// Note that if the node config said to not dynamically resolve and
	// update our public IP, [p.config.IPUdater] is a no-op implementation.
	go a.config.IPUpdater.Dispatch(log)

	if err := a.node.Initialize(&a.config, log, logFactory); err != nil {
		log.Fatal("error initializing node",
			zap.Error(err),
		)
		mapper.UnmapAllPorts()
		a.config.IPUpdater.Stop()
		log.Stop()
		logFactory.Close()
		return err
	}

	// [p.ExitCode] will block until [p.exitWG.Done] is called
	a.exitWG.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("caught panic", r)
			}
			log.Stop()
			logFactory.Close()
			a.exitWG.Done()
		}()
		defer func() {
			mapper.UnmapAllPorts()
			a.config.IPUpdater.Stop()

			// If [p.node.Dispatch()] panics, then we should log the panic and
			// then re-raise the panic. This is why the above defer is broken
			// into two parts.
			log.StopOnPanic()
		}()

		err := a.node.Dispatch()
		log.Debug("dispatch returned",
			zap.Error(err),
		)
	}()
	return nil
}

// Stop attempts to shutdown the currently running node. This function will
// return immediately.
func (a *app) Stop() error {
	a.node.Shutdown(0)
	return nil
}

// ExitCode returns the exit code that the node is reporting. This function
// blocks until the node has been shut down.
func (a *app) ExitCode() (int, error) {
	a.exitWG.Wait()
	return a.node.ExitCode(), nil
}
