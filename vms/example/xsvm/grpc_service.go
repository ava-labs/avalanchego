// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xsvm

import (
	"context"
	"errors"
	"io"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/proto/pb/xsvm"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ xsvm.PingServer = (*grpcService)(nil)

type grpcService struct {
	Log logging.Logger
	xsvm.UnimplementedPingServer
}

func (g *grpcService) Ping(_ context.Context, request *xsvm.PingRequest) (*xsvm.PingReply, error) {
	g.Log.Debug("ping", zap.String("message", request.Message))
	return &xsvm.PingReply{
		Message: request.Message,
	}, nil
}

func (g *grpcService) StreamPing(server xsvm.Ping_StreamPingServer) error {
	for {
		request, err := server.Recv()
		if errors.Is(err, io.EOF) {
			// Client closed the send stream
			return nil
		}
		if err != nil {
			return err
		}

		g.Log.Debug("stream ping", zap.String("message", request.Message))
		if err := server.Send(&xsvm.StreamPingReply{
			Message: request.Message,
		}); err != nil {
			return err
		}
	}
}
