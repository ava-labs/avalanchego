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
	"github.com/ava-labs/avalanchego/utils/perms"
)

var (
	exitCode = 0
)

// main runs an AvalancheGo node.
// If specified in the config, serves a hashicorp plugin that can be consumed by the daemon
// (see avalanchego/main).
func main() {
	defer func() {
		os.Exit(exitCode)
	}()

	c, _, err := config.GetConfig()
	if err != nil {
		exitCode = 1
		fmt.Printf("couldn't get node config: %s", err)
		return
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(c.DBPath, true, perms.ReadWriteExecute); err != nil {
		exitCode = 1
		fmt.Printf("failed to restrict the permissions of the database directory with error %s\n", err)
		return
	}
	if err := perms.ChmodR(c.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		exitCode = 1
		fmt.Printf("failed to restrict the permissions of the log directory with error %s\n", err)
		return
	}

	app := process.NewApp(c) // Create node wrapper
	if c.PluginMode {        // Serve as a plugin
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: appPlugin.Handshake,
			Plugins: map[string]plugin.Plugin{
				"nodeProcess": appPlugin.New(app),
			},
			GRPCServer: plugin.DefaultGRPCServer, // A non-nil value here enables gRPC serving for this plugin
			Logger: hclog.New(&hclog.LoggerOptions{
				Level: hclog.Error,
			}),
		})
		return
	}
	exitCode = app.Start() // Start the node
}
