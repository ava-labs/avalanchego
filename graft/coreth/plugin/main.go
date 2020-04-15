package main

import (
	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/vms/rpcchainvm"
	"github.com/ava-labs/gecko/x/plugin/evm"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: rpcchainvm.Handshake,
		Plugins: map[string]plugin.Plugin{
			"vm": rpcchainvm.New(&evm.VM{}),
		},

		// A non-nil value here enables gRPC serving for this plugin...
		GRPCServer: plugin.DefaultGRPCServer,
	})
}
