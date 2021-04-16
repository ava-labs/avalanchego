// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"

	appproto "github.com/ava-labs/avalanchego/app/plugin/proto"
	"github.com/ava-labs/avalanchego/app/process"
)

// Server wraps a node so it can be served with the hashicorp plugin harness
type Server struct {
	app *process.App
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(app *process.App) *Server {
	return &Server{
		app: app,
	}
}

func (ns *Server) Start(_ context.Context, req *appproto.StartRequest) (*appproto.StartResponse, error) {
	exitCode := ns.app.Start()
	return &appproto.StartResponse{ExitCode: int32(exitCode)}, nil
}

func (ns *Server) Stop(_ context.Context, req *appproto.StopRequest) (*appproto.StopResponse, error) {
	return &appproto.StopResponse{}, nil
}
