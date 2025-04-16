// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	messengerpb "github.com/ava-labs/avalanchego/proto/pb/messenger"
)

var (
	_ messengerpb.MessengerServer = (*Server)(nil)
)

// Server is a messenger that is managed over RPC.
type Server struct {
	messengerpb.UnsafeMessengerServer
	*common.SimpleSubscriber
}

// NewServer returns a messenger connected to a remote channel
func NewServer() *Server {
	return &Server{SimpleSubscriber: common.NewSimpleSubscriber()}
}

func (s *Server) Notify(_ context.Context, req *messengerpb.NotifyRequest) (*messengerpb.NotifyResponse, error) {
	msg := common.Message(req.Message)
	s.Publish(msg)
	return &messengerpb.NotifyResponse{}, nil
}
