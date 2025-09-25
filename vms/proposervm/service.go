// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
	avajson "github.com/ava-labs/avalanchego/utils/json"
)

var _ proposervmconnect.ProposerVMHandler = (*grpcService)(nil)

type grpcService struct {
	vm *VM
}

func (s *grpcService) GetProposedHeight(ctx context.Context, r *connect.Request[pb.GetProposedHeightRequest]) (*connect.Response[pb.GetProposedHeightReply], error) {
	log := s.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
		zap.Strings("route", r.Header()[server.HTTPHeaderRoute]),
	)
	log.Debug("API called")

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	blk, err := s.vm.getBlock(ctx, s.vm.preferred)
	if err != nil {
		log.Error("failed to get preferred block", zap.Error(err))
		return nil, fmt.Errorf("failed to get preferred block: %w", err)
	}

	height, err := blk.selectChildPChainHeight(ctx)
	if err != nil {
		log.Error("failed to get child p-chain height", zap.Error(err))
		return nil, fmt.Errorf("failed to get child p-chain height: %w", err)
	}

	return connect.NewResponse(&pb.GetProposedHeightReply{
		Height: height,
	}), nil
}

type jsonService struct {
	vm *VM
}

func (s *jsonService) GetProposedHeight(r *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	log := s.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
		zap.String("path", r.URL.Path),
	)
	log.Debug("API called")

	s.vm.ctx.Lock.Lock()
	defer s.vm.ctx.Lock.Unlock()

	ctx := r.Context()
	blk, err := s.vm.getBlock(ctx, s.vm.preferred)
	if err != nil {
		log.Error("failed to get preferred block", zap.Error(err))
		return fmt.Errorf("failed to get preferred block: %w", err)
	}

	height, err := blk.selectChildPChainHeight(ctx)
	if err != nil {
		log.Error("failed to get child p-chain height", zap.Error(err))
		return fmt.Errorf("failed to get child p-chain height: %w", err)
	}

	reply.Height = avajson.Uint64(height)
	return nil
}
