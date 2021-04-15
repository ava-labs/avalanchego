package process

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/nat"
	"github.com/ava-labs/avalanchego/node"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/dynamicip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

var (
	prevDBVersion = version.NewDefaultVersion(1, 0, 0)
	dbVersion     = version.NewDefaultVersion(1, 1, 0)
)

var (
	stakingPortName = fmt.Sprintf("%s-staking", constants.AppName)
	httpPortName    = fmt.Sprintf("%s-http", constants.AppName)
)

type App struct {
	config node.Config
	node   *node.Node // set in Start()
	log    logging.Logger
}

func NewApp(config node.Config) *App {
	return &App{
		config: config,
	}
}

func (a *App) Start() int {
	// we want to create the logger after the plugin as started the app
	logFactory := logging.NewFactory(a.config.LoggingConfig)
	defer logFactory.Close()

	var err error
	a.log, err = logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return 1
	}

	// startup the dbManager
	var dbManager manager.Manager
	if a.config.DBEnabled {
		dbManager, err = manager.New(a.config.DBPath, a.log, dbVersion, !a.config.FetchOnly)
		if err != nil {
			a.log.Error("couldn't create db manager at %s: %s", a.config.DBPath, err)
			return 1
		}
	} else {
		dbManager, err = manager.NewManagerFromDBs([]*manager.VersionedDatabase{
			{
				Database: memdb.New(),
				Version:  dbVersion,
			},
		})
		if err != nil {
			a.log.Error("couldn't create db manager from memory db: %s", err)
			return 1
		}
	}

	// ensures migrations are done
	exitcode, err := a.migrate(dbManager)
	if err != nil {
		a.log.Error("error handling migration %v", err)
		return exitcode
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered panic from", r)
		}
	}()

	defer func() {
		if err := dbManager.Shutdown(); err != nil {
			a.log.Warn("failed to close the node's DB: %s", err)
		}
		a.log.StopOnPanic()
		a.log.Stop()
	}()

	// Track if sybil control is enforced
	if !a.config.EnableStaking && a.config.EnableP2PTLS {
		a.log.Warn("Staking is disabled. Sybil control is not enforced.")
	}
	if !a.config.EnableStaking && !a.config.EnableP2PTLS {
		a.log.Warn("Staking and p2p encryption are disabled. Packet spoofing is possible.")
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
	if err := a.node.Initialize(&a.config, dbManager, a.log, logFactory); err != nil {
		a.log.Error("error initializing node: %s", err)
		return 1
	}

	err = a.node.Dispatch()
	a.log.Debug("node dispatch returned: %s", err)
	return a.node.ExitCode()
}

// Assumes [a.node] is not nil
func (a *App) Stop() int {
	a.node.Shutdown(0)
	a.node.DoneShuttingDown.Wait()
	return 0
}
