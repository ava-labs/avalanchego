// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plugin

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/app"

	pluginpb "github.com/ava-labs/avalanchego/proto/pb/plugin"
)

// Server wraps a node so it can be served with the hashicorp plugin harness
type Server struct {
	pluginpb.UnsafeNodeServer
	app app.App
}

func NewServer(app app.App) *Server {
	return &Server{
		app: app,
	}
}

func (s *Server) Start(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.app.Start()
}

func (s *Server) Stop(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.app.Stop()
}

func (s *Server) ExitCode(context.Context, *emptypb.Empty) (*pluginpb.ExitCodeResponse, error) {
	exitCode, err := s.app.ExitCode()
	return &pluginpb.ExitCodeResponse{
		ExitCode: int32(exitCode),
	}, err
}
