// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/app/process"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/perms"
	"github.com/ava-labs/avalanchego/version"

	appPlugin "github.com/ava-labs/avalanchego/app/plugin"
)

// GitCommit should be optionally set at compile time.
var GitCommit string

// main runs an AvalancheGo node.
// If specified in the config, serves a hashicorp plugin that can be consumed by
// the daemon (see avalanchego/main).
func main() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])
	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}
	processConfig, err := config.GetProcessConfig(v)
	if err != nil {
		fmt.Printf("couldn't load process config: %s\n", err)
		os.Exit(1)
	}
	nodeConfig, err := config.GetNodeConfig(v, processConfig.BuildDir)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	if processConfig.DisplayVersionAndExit {
		fmt.Print(version.String(GitCommit))
		os.Exit(0)
	}

	// Set the data directory permissions to be read write.
	if err := perms.ChmodR(nodeConfig.DBPath, true, perms.ReadWriteExecute); err != nil {
		fmt.Printf("failed to restrict the permissions of the database directory with error %s\n", err)
		os.Exit(1)
	}
	if err := perms.ChmodR(nodeConfig.LoggingConfig.Directory, true, perms.ReadWriteExecute); err != nil {
		fmt.Printf("failed to restrict the permissions of the log directory with error %s\n", err)
		os.Exit(1)
	}

	app := process.NewApp(nodeConfig) // Create node wrapper

	if processConfig.PluginMode { // Serve as a plugin
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

	fmt.Println(process.Header)

	_ = utils.HandleSignals(
		func(os.Signal) {
			app.Stop()
		},
		syscall.SIGINT, syscall.SIGTERM,
	)
	exitCode := app.Start() // Start the node
	os.Exit(exitCode)
}
