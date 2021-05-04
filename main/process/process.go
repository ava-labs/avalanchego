// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package process

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	stakingPortName = fmt.Sprintf("%s-staking", constants.AppName)
	httpPortName    = fmt.Sprintf("%s-http", constants.AppName)
)

// App is a wrapper around a node
type App struct {
	lock             sync.Mutex
	started, stopped bool
	config           node.Config
	node             *node.Node
	logFactory       logging.Factory
	log              logging.Logger
}

func NewApp(config node.Config, logFactory logging.Factory, log logging.Logger) *App {
	return &App{
		config:     config,
		node:       &node.Node{},
		log:        log,
		logFactory: logFactory,
	}
}

// Start the node. Returns the node's exit code when the node is done running.
// If Start() or Stop() have been called before, does nothing and returns 1.
func (a *App) Start() int {
	a.lock.Lock()
	if a.started {
		a.log.Error("can't start already started node")
		a.lock.Unlock()
		return 1
	} else if a.stopped {
		a.log.Error("can't start node that was previously stopped")
		a.lock.Unlock()
		return 1
	}
	a.started = true
	a.lock.Unlock()

	// start the db
	var (
		dbManager manager.Manager
		err       error
	)
	if a.config.DBEnabled {
		dbManager, err = manager.New(a.config.DBPath, a.log, node.DatabaseVersion)
		if err != nil {
			a.log.Error("couldn't create db manager at %s: %s", a.config.DBPath, err)
			return 1
		}
	} else {
		dbManager, err = manager.NewManagerFromDBs([]*manager.VersionedDatabase{
			{
				Database: memdb.New(),
				Version:  node.DatabaseVersion,
			},
		})
		if err != nil {
			a.log.Error("couldn't create db manager from memory db: %s", err)
			return 1
		}
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
	crypto.EnableCrypto = a.config.EnableCrypto

	if err := a.config.ConsensusParams.Valid(); err != nil {
		a.log.Error("consensus parameters are invalid: %s", err)
		return 1
	}

	// Track if assertions should be executed
	if a.config.LoggingConfig.Assertions {
		a.log.Debug("assertions are enabled. This may slow down execution")
	}

	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if a.config.AttemptedNATTraversal && !a.config.Nat.SupportsNAT() {
		a.log.Error("UPnP or NAT-PMP router attach failed, you may not be listening publicly," +
			" please confirm the settings in your router")
	}

	mapper := nat.NewPortMapper(a.log, a.config.Nat)
	defer mapper.UnmapAllPorts()

	// Open staking port we want for NAT Traversal to have the external port
	// (config.StakingIP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	mapper.Map(
		"TCP",
		a.config.StakingIP.IP().Port,
		a.config.StakingIP.IP().Port,
		stakingPortName,
		&a.config.StakingIP,
		a.config.DynamicUpdateDuration,
	)

	// Open the HTTP port iff the HTTP server is not listening on localhost
	if a.config.HTTPHost != "127.0.0.1" && a.config.HTTPHost != "localhost" {
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
		&a.config.StakingIP,
	)
	defer externalIPUpdater.Stop()

	a.log.Info("this node's IP is set to: %s", a.config.StakingIP.IP())
	a.node = &node.Node{}

	if err := a.node.Initialize(&a.config, dbManager, a.log, a.logFactory); err != nil {
		a.log.Error("error initializing node: %s", err)
		return 1
	}

	err = a.node.Dispatch()
	a.log.Debug("node dispatch returned: %s", err)
	return 0
}

// Assumes [a.node] is not nil
func (a *App) Stop() {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.stopped {
		a.log.Error("can't stop already stopped node")
		return
	}
	a.stopped = true
	if !a.started {
		// The node hasn't started yet. There's nothing to do.
		return
	}
	a.node.Shutdown()
}
