// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package appsender

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/appsender/appsenderproto"
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

func (s *Server) SendAppRequest(_ context.Context, req *appsenderproto.SendAppRequestMsg) (*appsenderproto.EmptyMsg, error) {
	nodeIDs := ids.NewShortSet(len(req.NodeIDs))
	for _, nodeIDBytes := range req.NodeIDs {
		nodeID, err := ids.ToShortID(nodeIDBytes)
		if err != nil {
			return nil, err
		}
		nodeIDs.Add(nodeID)
	}
	err := s.appSender.SendAppRequest(nodeIDs, req.RequestID, req.Request)
	return &appsenderproto.EmptyMsg{}, err
}

func (s *Server) SendAppResponse(_ context.Context, req *appsenderproto.SendAppResponseMsg) (*appsenderproto.EmptyMsg, error) {
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	err = s.appSender.SendAppResponse(nodeID, req.RequestID, req.Response)
	return &appsenderproto.EmptyMsg{}, err
}

func (s *Server) SendAppGossip(_ context.Context, req *appsenderproto.SendAppGossipMsg) (*appsenderproto.EmptyMsg, error) {
	err := s.appSender.SendAppGossip(req.Msg)
	return &appsenderproto.EmptyMsg{}, err
}
