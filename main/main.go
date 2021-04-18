// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"github.com/ava-labs/avalanchego/utils"
	"syscall"

	"os"

	"github.com/ava-labs/avalanchego/main/process"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	appPlugin "github.com/ava-labs/avalanchego/main/plugin"
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

	// parse config using viper
	if err := parseViper(); err != nil {
		fmt.Printf("parsing parameters returned with error %s\n", err)
		return
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(defaultDataDir, true, perms.ReadWriteExecute); err != nil {
		fmt.Printf("failed to restrict the permissions of the data directory with error %s\n", err)
		return
	}

	c := Config

	app := process.NewApp(c)
	if c.PluginMode { // Defaults to run as an standalone

		_ = utils.HandleSignals(
			func(os.Signal) {
			},
			syscall.SIGINT, syscall.SIGTERM,
		)

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
	exitCode = app.Start()
}
