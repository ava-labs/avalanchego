// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/app/process"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	appPlugin "github.com/ava-labs/avalanchego/app/plugin"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/perms"
)

const (
	header = "" +
		`     _____               .__                       .__` + "\n" +
		`    /  _  \___  _______  |  | _____    ____   ____ |  |__   ____    ,_ o` + "\n" +
		`   /  /_\  \  \/ /\__  \ |  | \__  \  /    \_/ ___\|  |  \_/ __ \   / //\,` + "\n" +
		`  /    |    \   /  / __ \|  |__/ __ \|   |  \  \___|   Y  \  ___/    \>> |` + "\n" +
		`  \____|__  /\_/  (____  /____(____  /___|  /\___  >___|  /\___  >    \\` + "\n" +
		`          \/           \/          \/     \/     \/     \/     \/`
)

var (
	exitCode        = 0
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

// main is the primary entry point to Avalanche.
func main() {
	defer func() {
		os.Exit(exitCode)
	}()

	config, err := config.GetConfig()
	if err != nil {
		exitCode = 1
		fmt.Printf("couldn't get node config: %s", err)
		return
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(config.DBPath, true, perms.ReadWriteExecute); err != nil {
		fmt.Printf("failed to restrict the permissions of the database directory with error %s\n", err)
		return
	}
	if err := perms.ChmodR(config.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		fmt.Printf("failed to restrict the permissions of the log directory with error %s\n", err)
		return
	}

	app := process.NewApp(config)
	if true { // run as plugin flag otherwise run as a normal node
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: appPlugin.Handshake,
			Plugins: map[string]plugin.Plugin{
				"nodeProcess": appPlugin.New(app),
			},
			// A non-nil value here enables gRPC serving for this plugin
			GRPCServer: plugin.DefaultGRPCServer,
			Logger: hclog.New(&hclog.LoggerOptions{
				Level: hclog.Error,
			}),
		})
		return
	}
	app.Start()
}
