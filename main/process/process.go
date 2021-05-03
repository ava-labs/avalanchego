// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package process

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils"
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

func (a *App) Start() int {
	// Create the logger
	logFactory := logging.NewFactory(a.config.LoggingConfig)
	defer logFactory.Close()

	var err error
	a.log, err = logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return 1
	}

	// start the db
	var db database.Database
	if a.config.DBEnabled {
		db, err = leveldb.New(a.config.DBPath, a.log, 0, 0, 0)
		if err != nil {
			a.log.Error("couldn't open database at %s: %s", a.config.DBPath, err)
			return 1
		}
	} else {
		db = memdb.New()
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered panic from", r)
		}
	}()

	defer func() {
		if err := db.Close(); err != nil {
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
	restarter := &restarter{
		node:          a.node,
		shouldRestart: &utils.AtomicBool{},
	}
	if err := a.node.Initialize(&a.config, db, a.log, logFactory, restarter); err != nil {
		a.log.Error("error initializing node: %s", err)
		return 1
	}

	err = a.node.Dispatch()
	a.log.Debug("node dispatch returned: %s", err)
	return 0
}

// Assumes [a.node] is not nil
func (a *App) Stop() int {
	a.node.Shutdown()
	return 0
}

// restarter implements utils.Restarter
type restarter struct {
	node *node.Node
	// If true, node should restart after shutting down
	shouldRestart *utils.AtomicBool
}

// Restarter shuts down the node and marks that it should restart
// It is safe to call Restart multiple times
func (r *restarter) Restart() {
	r.shouldRestart.SetValue(true)
	// Shutdown is safe to call multiple times because it uses sync.Once
	r.node.Shutdown()
}
