package plugin

import (
	"context"

	appproto "github.com/ava-labs/avalanchego/main/plugin/proto"
	"github.com/ava-labs/avalanchego/main/process"
	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"
)

var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "NODE_PROCESS_PLUGIN",
	MagicCookieValue: "dynamic",
}

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	"nodeProcess": &AppPlugin{},
}

type AppPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	app *process.App
}

func New(app *process.App) *AppPlugin {
	return &AppPlugin{
		app: app,
	}
}

// GRPCServer registers a new GRPC server.
func (p *AppPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	appproto.RegisterNodeServer(s, NewServer(p.app, broker))
	return nil
}

// GRPCClient returns a new GRPC client
func (p *AppPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewClient(appproto.NewNodeClient(c), broker), nil
}
