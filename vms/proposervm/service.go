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

func (s *service) GetProposedHeight(ctx context.Context, _ *connect.Request[pb.GetProposedHeightRequest]) (*connect.Response[pb.GetProposedHeightReply], error) {
	log := s.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
	)

	log.Debug("Connect RPC called")

	id, err := s.vm.State.GetLastAccepted()
	if err != nil {
		log.Error("failed to get last accepted block", zap.Error(err))
		return nil, fmt.Errorf("failed to get last accepted block: %w", err)
	}

	blk, err := s.vm.getBlock(ctx, id)
	if err != nil {
		log.Error("failed to get block", zap.Error(err))
		return nil, fmt.Errorf("failed to get block: %w", err)
	}

	height, err := blk.selectChildPChainHeight(ctx)
	if err != nil {
		log.Error("failed to get child p-chain height", zap.Error(err))
		return nil, fmt.Errorf("failed to get child p-chain height: %w", err)
	}

	log.Debug("GetProposedHeight returning", zap.Uint64("height", height))
	return connect.NewResponse(&pb.GetProposedHeightReply{
		Height: height,
	}), nil
}
