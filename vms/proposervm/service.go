// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"
	"github.com/ava-labs/avalanchego/vms/proposervm/acp181"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"

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

func (c *connectrpcService) GetCurrentEpoch(ctx context.Context, r *connect.Request[pb.GetCurrentEpochRequest]) (*connect.Response[pb.GetCurrentEpochReply], error) {
	c.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getCurrentEpoch"),
		zap.Strings("route", r.Header()[server.HTTPHeaderRoute]),
	)

	epoch, err := c.vm.getCurrentEpoch(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't get current epoch: %w", err)
	}

	return connect.NewResponse(&pb.GetCurrentEpochReply{
		Number:       epoch.Number,
		StartTime:    epoch.StartTime,
		PChainHeight: epoch.PChainHeight,
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
	Number       avajson.Uint64 `json:"number"`
	StartTime    avajson.Uint64 `json:"startTime"`
	PChainHeight avajson.Uint64 `json:"pChainHeight"`
}

func (j *jsonrpcService) GetCurrentEpoch(r *http.Request, _ *struct{}, reply *GetEpochResponse) error {
	j.vm.ctx.Log.Debug("API called",
		zap.String("service", "proposervm"),
		zap.String("method", "getCurrentEpoch"),
	)

	epoch, err := j.vm.getCurrentEpoch(r.Context())
	if err != nil {
		return fmt.Errorf("couldn't get current epoch: %w", err)
	}

	reply.Number = avajson.Uint64(epoch.Number)
	reply.StartTime = avajson.Uint64(epoch.StartTime)
	reply.PChainHeight = avajson.Uint64(epoch.PChainHeight)
	return nil
}

func (vm *VM) getCurrentEpoch(ctx context.Context) (block.Epoch, error) {
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	blk, err := vm.getBlock(ctx, vm.preferred)
	if err != nil {
		return block.Epoch{}, fmt.Errorf("couldn't get preferred block: %w", err)
	}

	epoch, err := blk.pChainEpoch(ctx)
	if err != nil {
		return block.Epoch{}, fmt.Errorf("couldn't get preferred block epoch: %w", err)
	}

	pChainHeight, err := blk.pChainHeight(ctx)
	if err != nil {
		return block.Epoch{}, fmt.Errorf("couldn't get preferred block p-chain height: %w", err)
	}

	timestamp := blk.Timestamp()
	newTimestamp := vm.Time().Truncate(time.Second)
	if newTimestamp.Before(timestamp) {
		newTimestamp = timestamp
	}

	return acp181.Epoch(
		vm.Upgrades,
		pChainHeight,
		epoch,
		timestamp,
		newTimestamp,
	), nil
}
