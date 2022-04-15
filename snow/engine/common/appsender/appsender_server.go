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

package appsender

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/engine/common"

	appsenderpb "github.com/chain4travel/caminogo/proto/pb/appsender"
)

var _ appsenderpb.AppSenderServer = &Server{}

type Server struct {
	appsenderpb.UnimplementedAppSenderServer
	appSender common.AppSender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(appSender common.AppSender) *Server {
	return &Server{appSender: appSender}
}

func (s *Server) SendAppRequest(_ context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
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

func (s *Server) SendAppResponse(_ context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(_ context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	err := s.appSender.SendAppGossip(req.Msg)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossipSpecific(_ context.Context, req *appsenderpb.SendAppGossipSpecificMsg) (*emptypb.Empty, error) {
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
