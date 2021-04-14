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
	mustUpgradeMsg  = "\nThis version of AvalancheGo requires a database upgrade before running.\n" +
		"To do the database upgrade, restart this node with argument --fetch-only.\n" +
		"This will start the node in fetch only mode. It will bootstrap a new database version and then stop.\n" +
		"By default, this node will attempt to bootstrap from a node running on the same machine (localhost) with staking port 9651.\n" +
		"If no such node exists, fetch only mode will be unable to complete.\n" +
		"The node in fetch only mode will by default not interfere with the node already running.\n" +
		"When the node in fetch only mode finishes, stop the other node running on this computer and run without --fetch-only flag to run node normally.\n" +
		"Fetch only mode will not change this node's staking key/certificate.\n" +
		"Note that populating the new database version will approximately double the amount of disk space required by AvalancheGo.\n" +
		"Ensure that this computer has at least enough disk space available.\n" +
		"You should not delete the old database version unless advised to by the Avalanche team."
	upgradingMsg = "\nNode running in fetch only mode.\n" +
		"It will bootstrap a new database version and then stop.\n" +
		"By default, this node will attempt to bootstrap from a node running on the same machine (localhost) with staking port 9651.\n" +
		"If no such node exists, fetch only mode will be unable to complete.\n" +
		"The node in fetch only mode will not by default interfere with the node already running on this machine.\n" +
		"When the node in fetch only mode finishes, stop the other node running on this computer and run without --fetch-only flag to run node normally.\n" +
		"Fetch only mode will not change this node's staking key/certificate.\n" +
		"Note that populating the new database version will approximately double the amount of disk space required by AvalancheGo.\n" +
		"Ensure that this computer has at least enough disk space available.\n" +
		"You should not delete the old database version unless advised to by the Avalanche team.\n"
	alreadyUpgradedMsg = "fetch only mode done. Restart this node without --fetch-only to run normally\n"
)

type App struct {
	config node.Config
	node   *node.Node // set in Start()
}

func NewApp(config node.Config) *App {
	return &App{
		config: config,
	}
}

func (a *App) Start() int {
	config := a.config
	logFactory := logging.NewFactory(config.LoggingConfig)
	defer logFactory.Close()

	log, err := logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		return 1
	}
	var dbManager manager.Manager
	if config.DBEnabled {
		dbManager, err = manager.New(config.DBPath, log, dbVersion, !config.FetchOnly)
		if err != nil {
			log.Fatal("couldn't create db manager at %s: %s", config.DBPath, err)
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
			log.Fatal("couldn't create db manager from memory db: %s", err)
			return 1
		}
	}

	// Fetch only
	currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
	if err != nil {
		log.Error("couldn't get whether database version %s ever bootstrapped: %s", dbVersion, err)
		return 1
	}
	log.Info("bootstrapped with current database version: %v", currentDBBootstrapped)

	if config.FetchOnly {
		// Flag says to run in fetch only mode
		if currentDBBootstrapped {
			// We have already bootstrapped the current database
			log.Info(alreadyUpgradedMsg)
			return constants.ExitCodeDoneMigrating
		}
		log.Info(upgradingMsg)
	} else {
		prevDB, exists := dbManager.Previous()
		if !currentDBBootstrapped && exists && prevDB.Version.Compare(prevDBVersion) == 0 {
			// If we have the previous database version but not the current one then node
			// must run in fetch only mode (--fetch-only). The default behavior for a node in
			// fetch only mode is to bootstrap from a node on the same machine (127.0.0.1)
			// Tell the user to run in fetch only mode.
			log.Fatal(mustUpgradeMsg)
			return 1
		}
	}

	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered panic from", r)
		}
	}()

	defer func() {
		if err := dbManager.Shutdown(); err != nil {
			log.Warn("failed to close the node's DB: %s", err)
		}
		log.StopOnPanic()
		log.Stop()
	}()

	// Track if sybil control is enforced
	if !config.EnableStaking && config.EnableP2PTLS {
		log.Warn("Staking is disabled. Sybil control is not enforced.")
	}
	if !config.EnableStaking && !config.EnableP2PTLS {
		log.Warn("Staking and p2p encryption are disabled. Packet spoofing is possible.")
	}

	// Check if transaction signatures should be checked
	if !config.EnableCrypto {
		log.Warn("transaction signatures are not being checked")
	}
	crypto.EnableCrypto = config.EnableCrypto

	if err := config.ConsensusParams.Valid(); err != nil {
		log.Fatal("consensus parameters are invalid: %s", err)
		return 1
	}

	// Track if assertions should be executed
	if config.LoggingConfig.Assertions {
		log.Debug("assertions are enabled. This may slow down execution")
	}

	// SupportsNAT() for NoRouter is false.
	// Which means we tried to perform a NAT activity but we were not successful.
	if config.AttemptedNATTraversal && !config.Nat.SupportsNAT() {
		log.Error("UPnP or NAT-PMP router attach failed, you may not be listening publicly," +
			" please confirm the settings in your router")
	}

	mapper := nat.NewPortMapper(log, config.Nat)
	defer mapper.UnmapAllPorts()

	// Open staking port we want for NAT Traversal to have the external port
	// (config.StakingIP.Port) to connect to our internal listening port
	// (config.InternalStakingPort) which should be the same in most cases.
	mapper.Map(
		"TCP",
		config.StakingIP.IP().Port,
		config.StakingIP.IP().Port,
		stakingPortName,
		&config.StakingIP,
		config.DynamicUpdateDuration,
	)

	// Open the HTTP port iff the HTTP server is not listening on localhost
	if config.HTTPHost != "127.0.0.1" && config.HTTPHost != "localhost" {
		// For NAT Traversal we want to route from the external port
		// (config.ExternalHTTPPort) to our internal port (config.HTTPPort)
		mapper.Map(
			"TCP",
			config.HTTPPort,
			config.HTTPPort,
			httpPortName,
			nil,
			config.DynamicUpdateDuration,
		)
	}

	// Regularly updates our public IP (or does nothing, if configured that way)
	externalIPUpdater := dynamicip.NewDynamicIPManager(
		config.DynamicPublicIPResolver,
		config.DynamicUpdateDuration,
		log,
		&config.StakingIP,
	)
	defer externalIPUpdater.Stop()

	log.Info("this node's IP is set to: %s", config.StakingIP.IP())
	a.node = &node.Node{}
	if err := a.node.Initialize(&config, dbManager, log, logFactory); err != nil {
		log.Error("error initializing node: %s", err)
		return 1
	}

	err = a.node.Dispatch()
	log.Debug("node dispatch returned: %s", err)
	return a.node.ExitCode()
}

// Assumes [a.node] is not nil
func (a *App) Stop() int { // TODO remove return value?
	a.node.Shutdown(0)
	a.node.DoneShuttingDown.Wait()
	return 0
}
