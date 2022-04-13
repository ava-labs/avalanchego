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

package plugin

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/chain4travel/caminogo/api/proto/pluginproto"
	"github.com/chain4travel/caminogo/app"
)

// Server wraps a node so it can be served with the hashicorp plugin harness
type Server struct {
	pluginproto.UnimplementedNodeServer
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

func (s *Server) ExitCode(context.Context, *emptypb.Empty) (*pluginproto.ExitCodeResponse, error) {
	exitCode, err := s.app.ExitCode()
	return &pluginproto.ExitCodeResponse{
		ExitCode: int32(exitCode),
	}, err
}
