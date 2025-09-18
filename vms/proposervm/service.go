// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
)

var _ proposervmconnect.ProposerVMHandler = (*service)(nil)

type service struct {
	vm *VM
}

func (s *service) GetProposedHeight(ctx context.Context, req *connect.Request[pb.GetProposedHeightRequest]) (*connect.Response[pb.GetProposedHeightReply], error) {
	s.vm.ctx.Log.Info("GetProposedHeight called - proposer VM service is working")

	height, err := s.vm.ctx.ValidatorState.GetMinimumHeight(ctx)
	if err != nil {
		s.vm.ctx.Log.Error("failed to get minimum height", zap.Error(err))
		return nil, fmt.Errorf("could not get minimum height: %w", err)
	}

	s.vm.ctx.Log.Info("GetProposedHeight returning", zap.Uint64("height", height))
	return connect.NewResponse(&pb.GetProposedHeightReply{
		Height: height,
	}), nil
}
