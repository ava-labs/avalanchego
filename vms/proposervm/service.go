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

var _ proposervmconnect.ProposerVMHandler = (*connectrpcService)(nil)

type connectrpcService struct {
	vm *VM
}

func (c *connectrpcService) GetProposedHeight(ctx context.Context, r *connect.Request[pb.GetProposedHeightRequest]) (*connect.Response[pb.GetProposedHeightReply], error) {
	log := c.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
		zap.Strings("route", r.Header()[server.HTTPHeaderRoute]),
	)
	log.Debug("API called")

	c.vm.ctx.Lock.Lock()
	defer c.vm.ctx.Lock.Unlock()

	blk, err := c.vm.getBlock(ctx, c.vm.preferred)
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

type jsonrpcService struct {
	vm *VM
}

func (j *jsonrpcService) GetProposedHeight(r *http.Request, _ *struct{}, reply *api.GetHeightResponse) error {
	log := j.vm.ctx.Log.With(
		zap.String("service", "proposervm"),
		zap.String("method", "GetProposedHeight"),
		zap.String("path", r.URL.Path),
	)
	log.Debug("API called")

	j.vm.ctx.Lock.Lock()
	defer j.vm.ctx.Lock.Unlock()

	ctx := r.Context()
	blk, err := j.vm.getBlock(ctx, j.vm.preferred)
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

type GetEpochResponse struct {
	Number       avajson.Uint64 `json:"height"`
	StartTime    avajson.Uint64 `json:"startTime"`
	PChainHeight avajson.Uint64 `json:"pChainHeight"`
}

// Returns the epoch information that will be used for the next block to be proposed.
func (j *jsonrpcService) GetCurrentEpoch(r *http.Request, _ *struct{}, reply *GetEpochResponse) error {
	j.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getCurrentEpoch"),
	)

	// This will error if we haven't advanced past the genesis block.
	lastAccepted, err := j.vm.GetLastAccepted()
	if err != nil {
		return fmt.Errorf("couldn't get last accepted block ID %w", err)
	}

	latestBlock, err := j.vm.getPostForkBlock(r.Context(), lastAccepted)
	if err != nil {
		return fmt.Errorf("couldn't get latest block %s: %w", lastAccepted.String(), err)
	}

	epoch, err := latestBlock.pChainEpoch(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get latest block epoch %s: %w", lastAccepted.String(), err)
	}

	pChainHeight, err := latestBlock.pChainHeight(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get latest block p-chain height %s: %w", lastAccepted.String(), err)
	}

	nextEpoch := nextPChainEpoch(
		pChainHeight,
		epoch,
		latestBlock.Timestamp(),
		j.vm.Upgrades.GraniteEpochDuration,
	)

	// Latest block sealed the epoch.
	reply.Number = avajson.Uint64(nextEpoch.Number)
	reply.StartTime = avajson.Uint64(nextEpoch.StartTime)
	reply.PChainHeight = avajson.Uint64(nextEpoch.PChainHeight)

	return nil
}
