// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/proto/pluginproto"
	"github.com/ava-labs/avalanchego/app"
)

const (
	Name = "nodeProcess"
)

var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  2,
	MagicCookieKey:   "NODE_PROCESS_PLUGIN",
	MagicCookieValue: "dynamic",
}

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	Name: &AppPlugin{},
}

// AppPlugin is can be served/consumed with the hashicorp plugin library.
// Plugin implements plugin.GRPCPlugin
type AppPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	app app.App
}

func New(app app.App) *AppPlugin {
	return &AppPlugin{
		app: app,
	}
}

// GRPCServer registers a new GRPC server.
func (p *AppPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pluginproto.RegisterNodeServer(s, NewServer(p.app))
	return nil
}

// GRPCClient returns a new GRPC client
func (p *AppPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewClient(pluginproto.NewNodeClient(c)), nil
}
