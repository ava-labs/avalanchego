// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/chain4travel/caminogo/snow/engine/snowman/block"
	"github.com/chain4travel/caminogo/vms/rpcchainvm/grpcutils"

	vmpb "github.com/chain4travel/caminogo/proto/pb/vm"
)

// protocolVersion should be bumped anytime changes are made which require
// the plugin vm to upgrade to latest avalanchego release to be compatible.
const protocolVersion = 12

var (
	// Handshake is a common handshake that is shared by plugin and host.
	Handshake = plugin.HandshakeConfig{
		ProtocolVersion:  protocolVersion,
		MagicCookieKey:   "VM_PLUGIN",
		MagicCookieValue: "dynamic",
	}

	// PluginMap is the map of plugins we can dispense.
	PluginMap = map[string]plugin.Plugin{
		"vm": &vmPlugin{},
	}

	_ plugin.Plugin     = &vmPlugin{}
	_ plugin.GRPCPlugin = &vmPlugin{}
)

type vmPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	// Concrete implementation, written in Go. This is only used for plugins
	// that are written in Go.
	vm block.ChainVM
}

// New will be called by the server side of the plugin to pass into the server
// side PluginMap for dispatching.
func New(vm block.ChainVM) plugin.Plugin {
	return &vmPlugin{vm: vm}
}

// GRPCServer registers a new GRPC server.
func (p *vmPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	vmpb.RegisterVMServer(s, NewServer(p.vm))
	return nil
}

// GRPCClient returns a new GRPC client
func (p *vmPlugin) GRPCClient(ctx context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewClient(vmpb.NewVMClient(c)), nil
}

// Serve serves a ChainVM plugin using sane gRPC server defaults.
func Serve(vm block.ChainVM) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins: map[string]plugin.Plugin{
			"vm": New(vm),
		},
		// ensure proper defaults
		GRPCServer: grpcutils.NewDefaultServer,
	})
}
