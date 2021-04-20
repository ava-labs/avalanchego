// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"log"
	"os"
	"syscall"

	appPlugin "github.com/ava-labs/avalanchego/main/plugin"
	"github.com/ava-labs/avalanchego/main/process"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

var (
	exitCode = 0
)

// main is the primary entry point to Avalanche.
func main() {
	defer func() {
		os.Exit(exitCode)
	}()

	// parse config using viper
	if err := parseViper(); err != nil {
		// Returns error code 1
		log.Fatalf("parsing parameters returned with error %s", err)
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(defaultDataDir, true, perms.ReadWriteExecute); err != nil {
		log.Fatalf("failed to restrict the permissions of the data directory with error %s", err)
	}

	c := Config

	app := process.NewApp(c)
	if c.PluginMode { // Defaults to run as an standalone
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

	_ = utils.HandleSignals(
		func(os.Signal) {
			app.Stop()
			os.Exit(exitCode)
		},
		syscall.SIGINT, syscall.SIGTERM,
	)
	// Start the node
	exitCode = app.Start()
}
