package process

import (
	"fmt"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	mustUpgradeMsg = "\nThis version of AvalancheGo requires a database upgrade before running.\n" +
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

func (a *App) migrate(dbManager manager.Manager) (int, error) {

	// should it use the fetch-only flag
	currentDBBootstrapped, err := dbManager.CurrentDBBootstrapped()
	if err != nil {
		return 1, fmt.Errorf("couldn't get whether database version %s ever bootstrapped: %s", dbVersion, err)
	}
	a.log.Info("bootstrapped with current database version: %v", currentDBBootstrapped)

	if a.config.FetchOnly {
		// Flag says to run in fetch only mode
		if currentDBBootstrapped {
			// We have already bootstrapped the current database
			a.log.Info(alreadyUpgradedMsg)
			return constants.ExitCodeDoneMigrating, nil
		}
		a.log.Info(upgradingMsg)
	} else {
		prevDB, exists := dbManager.Previous()
		if !currentDBBootstrapped && exists && prevDB.Version.Compare(prevDBVersion) == 0 {
			// If we have the previous database version but not the current one then node
			// must run in fetch only mode (--fetch-only). The default behavior for a node in
			// fetch only mode is to bootstrap from a node on the same machine (127.0.0.1)
			// Tell the user to run in fetch only mode.
			return 1, fmt.Errorf(mustUpgradeMsg)
		}
	}
	return 0, nil
}
