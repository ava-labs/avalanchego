// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"

	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
)

var _ appsenderpb.AppSenderServer = (*Server)(nil)

type Server struct {
	appsenderpb.UnsafeAppSenderServer
	appSender common.AppSender
}

// NewServer returns a messenger connected to a remote channel
func NewServer(appSender common.AppSender) *Server {
	return &Server{appSender: appSender}
}

func (s *Server) SendAppRequest(ctx context.Context, req *appsenderpb.SendAppRequestMsg) (*emptypb.Empty, error) {
	nodeIDs := set.NewSet[ids.NodeID](len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		shortNodeID, err := ids.ToShortNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(shortNodeID.NodeID())
	}
	err := s.appSender.SendAppRequest(ctx, nodeIDs, req.RequestId, req.Request)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppResponse(ctx context.Context, req *appsenderpb.SendAppResponseMsg) (*emptypb.Empty, error) {
	shortNodeID, err := ids.ToShortNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(ctx, shortNodeID.NodeID(), req.RequestId, req.Response)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppError(ctx context.Context, req *appsenderpb.SendAppErrorMsg) (*emptypb.Empty, error) {
	shortNodeID, err := ids.ToShortNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	err = s.appSender.SendAppError(ctx, shortNodeID.NodeID(), req.RequestId, req.ErrorCode, req.ErrorMessage)
	return &emptypb.Empty{}, err
}

func (s *Server) SendAppGossip(ctx context.Context, req *appsenderpb.SendAppGossipMsg) (*emptypb.Empty, error) {
	nodeIDs := set.NewSet[ids.NodeID](len(req.NodeIds))
	for _, nodeIDBytes := range req.NodeIds {
		shortNodeID, err := ids.ToShortNodeID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(shortNodeID.NodeID())
	}
	err := s.appSender.SendAppGossip(
		ctx,
		common.SendConfig{
			NodeIDs:       nodeIDs,
			Validators:    int(req.Validators),
			NonValidators: int(req.NonValidators),
			Peers:         int(req.Peers),
		},
		req.Msg,
	)
	return &emptypb.Empty{}, err
}
