// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package process

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/ava-labs/avalanchego/app"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/rocksdb"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/version"
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

	_ app.App = &process{}
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
// Does not block until the node is done.
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

	// start the db manager
	var dbManager manager.Manager
	switch p.config.DatabaseConfig.Name {
	case rocksdb.Name:
		path := filepath.Join(p.config.DatabaseConfig.Path, rocksdb.Name)
		dbManager, err = manager.NewRocksDB(path, p.config.DatabaseConfig.Config, log, version.CurrentDatabase)
	case leveldb.Name:
		dbManager, err = manager.NewLevelDB(p.config.DatabaseConfig.Path, p.config.DatabaseConfig.Config, log, version.CurrentDatabase)
	case memdb.Name:
		dbManager = manager.NewMemDB(version.CurrentDatabase)
	default:
		err = fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			p.config.DatabaseConfig.Name,
			leveldb.Name,
			rocksdb.Name,
			memdb.Name,
		)
	}
	if err != nil {
		log.Fatal("couldn't create %q db manager at %s: %s", p.config.DatabaseConfig.Name, p.config.DatabaseConfig.Path, err)
		logFactory.Close()
		return err
	}

	// Track if sybil control is enforced
	if !p.config.EnableStaking {
		log.Warn("Staking is disabled. Sybil control is not enforced.")
	}

	// Check if transaction signatures should be checked
	if !p.config.EnableCrypto {
		// TODO: actually disable crypto verification
		log.Warn("transaction signatures are not being checked")
	}

	// Track if assertions should be executed
	if p.config.LoggingConfig.Assertions {
		log.Debug("assertions are enabled. This may slow down execution")
	}

	// TODO move this to config
	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if p.config.AttemptedNATTraversal && !p.config.Nat.SupportsNAT() {
		log.Warn("UPnP or NAT-PMP router attach failed, you may not be listening publicly. " +
			"Please confirm the settings in your router")
	}

	mapper := nat.NewPortMapper(log, p.config.Nat)

	// Open staking port we want for NAT Traversal to have the external port
	// (config.IP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	if p.config.IP.IP().Port != 0 {
		mapper.Map(
			"TCP",
			p.config.IP.IP().Port,
			p.config.IP.IP().Port,
			stakingPortName,
			&p.config.IP,
			p.config.DynamicUpdateDuration,
		)
	}

	// Open the HTTP port iff the HTTP server is not listening on localhost
	if p.config.HTTPHost != "127.0.0.1" && p.config.HTTPHost != "localhost" && p.config.HTTPPort != 0 {
		// For NAT Traversal we want to route from the external port
		// (config.ExternalHTTPPort) to our internal port (config.HTTPPort)
		mapper.Map(
			"TCP",
			p.config.HTTPPort,
			p.config.HTTPPort,
			httpPortName,
			nil,
			p.config.DynamicUpdateDuration,
		)
	}

	// Regularly updates our public IP (or does nothing, if configured that way)
	externalIPUpdater := dynamicip.NewDynamicIPManager(
		p.config.DynamicPublicIPResolver,
		p.config.DynamicUpdateDuration,
		log,
		&p.config.IP,
	)

	if err := p.node.Initialize(&p.config, dbManager, log, logFactory); err != nil {
		log.Fatal("error initializing node: %s", err)
		mapper.UnmapAllPorts()
		externalIPUpdater.Stop()
		if err := dbManager.Close(); err != nil {
			log.Warn("failed to close the node's DB: %s", err)
		}
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
			externalIPUpdater.Stop()
			if err := dbManager.Close(); err != nil {
				log.Warn("failed to close the node's DB: %s", err)
			}

			// If [p.node.Dispatch()] panics, then we should log the panic and
			// then re-raise the panic. This is why the above defer is broken
			// into two parts.
			log.StopOnPanic()
		}()

		err := p.node.Dispatch()
		log.Debug("dispatch returned with: %s", err)
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
