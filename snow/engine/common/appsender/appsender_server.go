// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/api/proto/appsenderproto"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"google.golang.org/protobuf/types/known/emptypb"
)

var _ appsenderproto.AppSenderServer = &Server{}

type Server struct {
	appsenderproto.UnimplementedAppSenderServer
	appSender common.AppSender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(appSender common.AppSender) *Server {
	return &Server{appSender: appSender}
}

func (s *Server) SendAppRequest(_ context.Context, req *appsenderproto.SendAppRequestMsg) (*emptypb.Empty, error) {
	nodeIDs := ids.NewShortSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToShortID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppRequest(nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppResponse(_ context.Context, req *appsenderproto.SendAppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(_ context.Context, req *appsenderproto.SendAppGossipMsg) (*emptypb.Empty, error) {
	err := s.appSender.SendAppGossip(req.Msg)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossipSpecific(_ context.Context, req *appsenderproto.SendAppGossipSpecificMsg) (*emptypb.Empty, error) {
	nodeIDs := ids.NewShortSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToShortID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppGossipSpecific(nodeIDs, req.Msg)
	return &emptypb.Empty{}, err
}
