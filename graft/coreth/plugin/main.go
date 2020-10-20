package main

import (
	"fmt"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm"

	"github.com/ava-labs/coreth/plugin/evm"
)

func main() {
	if errs.Errored() {
		panic(fmt.Sprintf("Errored while parsing Coreth CLI Config: %w", errs.Err))
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: rpcchainvm.Handshake,
		Plugins: map[string]plugin.Plugin{
			"vm": rpcchainvm.New(&evm.VM{CLIConfig: cliConfig}),
		},

		// A non-nil value here enables gRPC serving for this plugin...
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
