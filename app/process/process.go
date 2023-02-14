// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package process

import (
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/app"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/ips"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/utils/ulimit"
)

const (
	Header = `     _____               .__                       .__
    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o
   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,
  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |
  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\
          \/           \/          \/     \/     \/     \/     \/`
)

var (
	stakingPortName = fmt.Sprintf("%s-staking", constants.AppName)
	httpPortName    = fmt.Sprintf("%s-http", constants.AppName)

	_ app.App = (*process)(nil)
)

// process is a wrapper around a node that runs in this process
type process struct {
	config node.Config
	node   *node.Node
	exitWG sync.WaitGroup
}

func NewApp(config node.Config) app.App {
	return &process{
		config: config,
		node:   &node.Node{},
	}
}

// Start the business logic of the node (as opposed to config reading, etc).
// Does not block until the node is done. Errors returned from this method
// are not logged.
func (p *process) Start() error {
	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(p.config.DatabaseConfig.Path, true, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to restrict the permissions of the database directory with: %w", err)
	}
	if err := perms.ChmodR(p.config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("failed to restrict the permissions of the log directory with: %w", err)
	}

	// we want to create the logger after the plugin has started the app
	logFactory := logging.NewFactory(p.config.LoggingConfig)
	log, err := logFactory.Make("main")
	if err != nil {
		logFactory.Close()
		return err
	}

	// update fd limit
	fdLimit := p.config.FdLimit
	if err := ulimit.Set(fdLimit, log); err != nil {
		log.Fatal("failed to set fd-limit",
			zap.Error(err),
		)
		logFactory.Close()
		return err
	}

	// Track if sybil control is enforced
	if !p.config.EnableStaking {
		log.Warn("sybil control is not enforced",
			zap.String("reason", "staking is disabled"),
		)
	}

	// TODO move this to config
	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if p.config.AttemptedNATTraversal && !p.config.Nat.SupportsNAT() {
		log.Warn("UPnP and NAT-PMP router attach failed, you may not be listening publicly. " +
			"Please confirm the settings in your router")
	}

	if ip := p.config.IPPort.IPPort().IP; ip.IsLoopback() || ip.IsPrivate() {
		log.Warn("P2P IP is private, you will not be publicly discoverable",
			zap.Stringer("ip", ip),
		)
	}

	// An empty host is treated as a wildcard to match all addresses, so it is
	// considered public.
	hostIsPublic := p.config.HTTPHost == ""
	if !hostIsPublic {
		ip, err := ips.Lookup(p.config.HTTPHost)
		if err != nil {
			log.Fatal("failed to lookup HTTP host",
				zap.String("host", p.config.HTTPHost),
				zap.Error(err),
			)
			logFactory.Close()
			return err
		}
		hostIsPublic = !ip.IsLoopback() && !ip.IsPrivate()

		log.Debug("finished HTTP host lookup",
			zap.String("host", p.config.HTTPHost),
			zap.Stringer("ip", ip),
			zap.Bool("isPublic", hostIsPublic),
		)
	}

	mapper := nat.NewPortMapper(log, p.config.Nat)

	// Open staking port we want for NAT traversal to have the external port
	// (config.IP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	if port := p.config.IPPort.IPPort().Port; port != 0 {
		mapper.Map(
			port,
			port,
			stakingPortName,
			p.config.IPPort,
			p.config.IPResolutionFreq,
		)
	}

	// Don't open the HTTP port if the HTTP server is private
	if hostIsPublic {
		log.Warn("HTTP server is binding to a potentially public host. "+
			"You may be vulnerable to a DoS attack if your HTTP port is publicly accessible",
			zap.String("host", p.config.HTTPHost),
		)

		// For NAT traversal we want to route from the external port
		// (config.ExternalHTTPPort) to our internal port (config.HTTPPort).
		if p.config.HTTPPort != 0 {
			mapper.Map(
				p.config.HTTPPort,
				p.config.HTTPPort,
				httpPortName,
				nil,
				p.config.IPResolutionFreq,
			)
		}
	}

	// Regularly update our public IP.
	// Note that if the node config said to not dynamically resolve and
	// update our public IP, [p.config.IPUdater] is a no-op implementation.
	go p.config.IPUpdater.Dispatch(log)

	if err := p.node.Initialize(&p.config, log, logFactory); err != nil {
		log.Fatal("error initializing node",
			zap.Error(err),
		)
		mapper.UnmapAllPorts()
		p.config.IPUpdater.Stop()
		log.Stop()
		logFactory.Close()
		return err
	}

	// [p.ExitCode] will block until [p.exitWG.Done] is called
	p.exitWG.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("caught panic", r)
			}
			log.Stop()
			logFactory.Close()
			p.exitWG.Done()
		}()
		defer func() {
			mapper.UnmapAllPorts()
			p.config.IPUpdater.Stop()

			// If [p.node.Dispatch()] panics, then we should log the panic and
			// then re-raise the panic. This is why the above defer is broken
			// into two parts.
			log.StopOnPanic()
		}()

		err := p.node.Dispatch()
		log.Debug("dispatch returned",
			zap.Error(err),
		)
	}()
	return nil
}

// Stop attempts to shutdown the currently running node. This function will
// return immediately.
func (p *process) Stop() error {
	p.node.Shutdown(0)
	return nil
}

// ExitCode returns the exit code that the node is reporting. This function
// blocks until the node has been shut down.
func (p *process) ExitCode() (int, error) {
	p.exitWG.Wait()
	return p.node.ExitCode(), nil
}
