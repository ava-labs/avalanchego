// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"log"
	"os"
	"syscall"

	appPlugin "github.com/ava-labs/avalanchego/main/plugin"
	"github.com/ava-labs/avalanchego/main/process"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
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

// main is the primary entry point to Avalanche.
func main() {
	// parse config using viper
	if err := parseViper(); err != nil {
		// Returns exit code 1
		log.Fatalf("parsing parameters returned with error %s", err)
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(defaultDataDir, true, perms.ReadWriteExecute); err != nil {
		log.Fatalf("failed to restrict the permissions of the data directory with error %s", err)
	}

	c := Config
	// Create the logger
	logFactory := logging.NewFactory(c.LoggingConfig)
	defer logFactory.Close()

	log, err := logFactory.Make()
	if err != nil {
		fmt.Printf("starting logger failed with: %s\n", err)
		os.Exit(1)
	}
	app := process.NewApp(c, logFactory, log)
	if c.PluginMode {
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

	fmt.Println(header)

	// If we get a a SIGINT or SIGTERM, tell the node to stop.
	// If [app.Start()] has been called, it will return.
	// If not, then when [app.Start()] is called below, it will immediately return 1.
	_ = utils.HandleSignals(
		func(os.Signal) {
			app.Stop()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)
	// Start the node
	exitCode := app.Start()
	os.Exit(exitCode)
}
