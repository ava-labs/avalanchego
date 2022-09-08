// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"go.opentelemetry.io/otel"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"

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

func (s *Server) SendAppRequest(ctx context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
	newCtx, span := otel.Tracer("TODO").Start(ctx, "Server.SendAppRequest")
	defer span.End()

	nodeIDs := ids.NewNodeIDSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppRequest(newCtx, nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppResponse(ctx context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	newCtx, span := otel.Tracer("TODO").Start(ctx, "Server.SendAppResponse")
	defer span.End()

	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(newCtx, nodeID, req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(ctx context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	newCtx, span := otel.Tracer("TODO").Start(ctx, "Server.SendAppGossip")
	defer span.End()

	err := s.appSender.SendAppGossip(newCtx, req.Msg)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossipSpecific(ctx context.Context, req *appsenderpb.SendAppGossipSpecificMsg) (*emptypb.Empty, error) {
	newCtx, span := otel.Tracer("TODO").Start(ctx, "Server.SendAppGossipSpecific")
	defer span.End()

	nodeIDs := ids.NewNodeIDSet(len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		nodeID, err := ids.ToNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppGossipSpecific(newCtx, nodeIDs, req.Msg)
	return &emptypb.Empty{}, err
}
