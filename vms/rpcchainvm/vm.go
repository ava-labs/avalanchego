// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/proto/vmproto"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	// Handshake is a common handshake that is shared by plugin and host.
	Handshake = plugin.HandshakeConfig{
		ProtocolVersion:  10,
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

// GRPCServer registers a new GRPC server.
func (p *vmPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	vmproto.RegisterVMServer(s, NewServer(p.vm, broker))
	return nil
}

// GRPCClient returns a new GRPC client
func (p *vmPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewClient(vmproto.NewVMClient(c), broker), nil
}
