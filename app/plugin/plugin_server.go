// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"
	appproto "github.com/ava-labs/avalanchego/app/plugin/proto"
	"github.com/ava-labs/avalanchego/app/process"

	"github.com/hashicorp/go-plugin"
)

type Server struct {
	app    *process.App
	broker *plugin.GRPCBroker
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(app *process.App, broker *plugin.GRPCBroker) *Server {
	return &Server{
		app:    app,
		broker: broker,
	}
}

func (ns *Server) Start(_ context.Context, req *appproto.StartRequest) (*appproto.StartResponse, error) {
	return nil, ns.app.Start()
}

func (ns *Server) Stop(_ context.Context, req *appproto.StopRequest) (*appproto.StopResponse, error) {
	return &appproto.StopResponse{ExitCode: int32(ns.app.Stop())}, nil
}
