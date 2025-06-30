// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/api/grpcclient"
	"github.com/ava-labs/avalanchego/connectproto/pb/xsvm/xsvmconnect"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/xsvm"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ xsvmconnect.PingHandler = (*PingService)(nil)

type PingService struct {
	xsvm.UnsafePingServer

	Log logging.Logger
}

func (p PingService) Ping(_ context.Context, request *connect.Request[xsvm.PingRequest]) (*connect.Response[xsvm.PingReply], error) {
	p.Log.Debug("ping", zap.String("message", request.Msg.Message))
	return connect.NewResponse[xsvm.PingReply](
		&xsvm.PingReply{
			Message: request.Msg.Message,
		},
	), nil
}

func (p PingService) StreamPing(_ context.Context, server *connect.BidiStream[xsvm.StreamPingRequest, xsvm.StreamPingReply]) error {
	for {
		request, err := server.Receive()
		if errors.Is(err, io.EOF) {
			// Client closed the send stream
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to receive message: %w", err)
		}

		p.Log.Debug("stream ping", zap.String("message", request.Message))
		err = server.Send(&xsvm.StreamPingReply{
			Message: request.Message,
		})
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}
}

// NewPingClient returns a client for PingService
func NewPingClient(
	uri string,
	chainID ids.ID,
	opts ...grpc.DialOption,
) (xsvm.PingClient, *grpc.ClientConn, error) {
	conn, err := grpcclient.NewChainClient(uri, chainID, opts...)
	if err != nil {
		return nil, nil, err
	}

	return xsvm.NewPingClient(conn), conn, nil
}
