// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runner

import (
	"fmt"
	"os"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/app"
	"github.com/ava-labs/avalanchego/app/process"
	"github.com/ava-labs/avalanchego/node"

	appplugin "github.com/ava-labs/avalanchego/app/plugin"
)

// Run an AvalancheGo node.
// If specified in the config, serves a hashicorp plugin that can be consumed by
// the daemon (see avalanchego/main).
func Run(config Config, nodeConfig node.Config) {
	nodeApp := process.NewApp(nodeConfig) // Create node wrapper
	if config.PluginMode {                // Serve as a plugin
		plugin.Serve(&plugin.ServeConfig{
			HandshakeConfig: appplugin.Handshake,
			Plugins: map[string]plugin.Plugin{
				appplugin.Name: appplugin.New(nodeApp),
			},
			GRPCServer: plugin.DefaultGRPCServer, // A non-nil value here enables gRPC serving for this plugin
			Logger: hclog.New(&hclog.LoggerOptions{
				Level: hclog.Error,
			}),
		})
		return
	}

	fmt.Println(process.Header)

	exitCode := app.Run(nodeApp)
	os.Exit(exitCode)
}
