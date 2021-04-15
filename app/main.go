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

// main is the primary entry point to Avalanche.
func main() {
	defer func() {
		os.Exit(exitCode)
	}()

	c, err := config.GetConfig()
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

	app := process.NewApp(c)
	if c.PluginRun { // defaults to run as plugin
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
	exitCode = app.Start()
}
