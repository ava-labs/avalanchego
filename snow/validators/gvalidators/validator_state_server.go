// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

var _ pb.ValidatorStateServer = &Server{}

type Server struct {
	pb.UnsafeValidatorStateServer
	state validators.State
}

func NewServer(state validators.State) *Server {
	return &Server{state: state}
}

func (s *Server) GetMinimumHeight(ctx context.Context, msg *emptypb.Empty) (*pb.GetMinimumHeightResponse, error) {
	height, err := s.state.GetMinimumHeight()
	return &pb.GetMinimumHeightResponse{Height: height}, err
}

func (s *Server) GetCurrentHeight(ctx context.Context, msg *emptypb.Empty) (*pb.GetCurrentHeightResponse, error) {
	height, err := s.state.GetCurrentHeight()
	return &pb.GetCurrentHeightResponse{Height: height}, err
}

func (s *Server) GetValidatorSet(_ context.Context, req *pb.GetValidatorSetRequest) (*pb.GetValidatorSetResponse, error) {
	subnetID, err := ids.ToID(req.SubnetId)
	if err != nil {
		return nil, err
	}

	vdrs, err := s.state.GetValidatorSet(req.Height, subnetID)
	if err != nil {
		return nil, err
	}

	resp := &pb.GetValidatorSetResponse{
		Validators: make([]*pb.Validator, len(vdrs)),
	}

	i := 0
	for nodeID, weight := range vdrs {
		nodeID := nodeID
		resp.Validators[i] = &pb.Validator{
			NodeId: nodeID[:],
			Weight: weight,
		}
		i++
	}
	return resp, nil
}
