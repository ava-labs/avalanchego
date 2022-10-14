// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"

	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
)

var _ appsenderpb.AppSenderServer = &Server{}

type Server struct {
	appsenderpb.UnsafeAppSenderServer
	appSender common.AppSender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(appSender common.AppSender) *Server {
	return &Server{appSender: appSender}
}

func (s *Server) SendCrossChainAppRequest(parentCtx context.Context, msg *appsenderpb.SendCrossChainAppRequestMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendCrossChainAppRequest")
	defer span.End()

	chainID, err := ids.ToID(msg.ChainId)
	if err != nil {
		return &emptypb.Empty{}, err
	}

	return &emptypb.Empty{}, s.appSender.SendCrossChainAppRequest(ctx, chainID, msg.RequestId, msg.Request)
}

func (s *Server) SendCrossChainAppResponse(parentCtx context.Context, msg *appsenderpb.SendCrossChainAppResponseMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendCrossChainAppResponse")
	defer span.End()

	chainID, err := ids.ToID(msg.ChainId)
	if err != nil {
		return &emptypb.Empty{}, err
	}

	return &emptypb.Empty{}, s.appSender.SendCrossChainAppResponse(ctx, chainID, msg.RequestId, msg.Response)
}

func (s *Server) SendAppRequest(parentCtx context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendAppRequest")
	defer span.End()

	nodeIDs := ids.NewNodeIDSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppRequest(ctx, nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppResponse(parentCtx context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendAppResponse")
	defer span.End()

	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(ctx, nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(parentCtx context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendAppGossip")
	defer span.End()

	err := s.appSender.SendAppGossip(ctx, req.Msg)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossipSpecific(parentCtx context.Context, req *appsenderpb.SendAppGossipSpecificMsg) (*emptypb.Empty, error) {
	ctx, span := trace.Tracer().Start(parentCtx, "Server.SendAppGossipSpecific")
	defer span.End()

	nodeIDs := ids.NewNodeIDSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppGossipSpecific(ctx, nodeIDs, req.Msg)
	return &emptypb.Empty{}, err
}
