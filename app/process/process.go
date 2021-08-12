// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package process

import (
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/rocksdb"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
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
)

// App is a wrapper around a node
type App struct {
	config node.Config
	node   *node.Node
	// log is set in Start()
	log logging.Logger
}

func NewApp(config node.Config) *App {
	return &App{
		config: config,
		node:   &node.Node{},
	}
}

// Start creates and runs an AvalancheGo node.
// Returns the node's exit code. If [a.Stop()] is called, Start()
// returns 0. This method blocks until the node is done.
func (a *App) Start() int {
	// we want to create the logger after the plugin has started the app
	logFactory := logging.NewFactory(a.config.LoggingConfig)
	defer logFactory.Close()

	var err error
	a.log, err = logFactory.Make("main")
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return 1
	}

	// start the db manager
	var dbManager manager.Manager
	switch a.config.DatabaseConfig.Name {
	case rocksdb.Name:
		path := filepath.Join(a.config.DatabaseConfig.Path, rocksdb.Name)
		dbManager, err = manager.NewRocksDB(path, a.log, version.CurrentDatabase)
	case leveldb.Name:
		dbManager, err = manager.NewLevelDB(a.config.DatabaseConfig.Path, a.log, version.CurrentDatabase)
	case memdb.Name:
		dbManager = manager.NewMemDB(version.CurrentDatabase)
	default:
		err = fmt.Errorf(
			"db-type was %q but should have been one of {%s, %s, %s}",
			a.config.DatabaseConfig.Name,
			leveldb.Name,
			rocksdb.Name,
			memdb.Name,
		)
	}
	if err != nil {
		a.log.Fatal("couldn't create %q db manager at %s: %s", a.config.DatabaseConfig.Name, a.config.DatabaseConfig.Path, err)
		return 1
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered panic from", r)
		}
	}()

	defer func() {
		if err := dbManager.Close(); err != nil {
			a.log.Warn("failed to close the node's DB: %s", err)
		}
		a.log.StopOnPanic()
		a.log.Stop()
	}()

	// Track if sybil control is enforced
	if !a.config.EnableStaking {
		a.log.Warn("Staking is disabled. Sybil control is not enforced.")
	}

	// Check if transaction signatures should be checked
	if !a.config.EnableCrypto {
		a.log.Warn("transaction signatures are not being checked")
	}
	// TODO: disable crypto verification

	// Track if assertions should be executed
	if a.config.LoggingConfig.Assertions {
		a.log.Debug("assertions are enabled. This may slow down execution")
	}

	// TODO move this to config
	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if a.config.AttemptedNATTraversal && !a.config.Nat.SupportsNAT() {
		a.log.Warn("UPnP or NAT-PMP router attach failed, you may not be listening publicly. " +
			"Please confirm the settings in your router")
	}

	mapper := nat.NewPortMapper(a.log, a.config.Nat)
	defer mapper.UnmapAllPorts()

	// Open staking port we want for NAT Traversal to have the external port
	// (config.IP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	if a.config.IP.IP().Port != 0 {
		mapper.Map(
			"TCP",
			a.config.IP.IP().Port,
			a.config.IP.IP().Port,
			stakingPortName,
			&a.config.IP,
			a.config.DynamicUpdateDuration,
		)
	}

	// Open the HTTP port iff the HTTP server is not listening on localhost
	if a.config.HTTPHost != "127.0.0.1" && a.config.HTTPHost != "localhost" && a.config.HTTPPort != 0 {
		// For NAT Traversal we want to route from the external port
		// (config.ExternalHTTPPort) to our internal port (config.HTTPPort)
		mapper.Map(
			"TCP",
			a.config.HTTPPort,
			a.config.HTTPPort,
			httpPortName,
			nil,
			a.config.DynamicUpdateDuration,
		)
	}

	// Regularly updates our public IP (or does nothing, if configured that way)
	externalIPUpdater := dynamicip.NewDynamicIPManager(
		a.config.DynamicPublicIPResolver,
		a.config.DynamicUpdateDuration,
		a.log,
		&a.config.IP,
	)
	defer externalIPUpdater.Stop()

	if err := a.node.Initialize(&a.config, dbManager, a.log, logFactory); err != nil {
		a.log.Fatal("error initializing node: %s", err)
		return 1
	}

	err = a.node.Dispatch()
	a.log.Debug("node dispatch returned: %s", err)
	return a.node.ExitCode()
}

// Assumes [a.node] is not nil.
// Blocks until [a.node] is done shutting down.
func (a *App) Stop() {
	a.node.Shutdown(0)
	a.node.DoneShuttingDown.Wait()
}
