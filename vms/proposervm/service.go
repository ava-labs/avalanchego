// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"fmt"

	"connectrpc.com/connect"

	"github.com/ava-labs/avalanchego/connectproto/pb/proposervm/proposervmconnect"

	pb "github.com/ava-labs/avalanchego/connectproto/pb/proposervm"
)

var _ proposervmconnect.ProposerVMHandler = (*service)(nil)

type service struct {
	vm *VM
}

func (p *service) GetProposedHeight(ctx context.Context, _ *connect.Request[pb.GetProposedHeightRequest]) (*connect.Response[pb.GetProposedHeightReply], error) {
	p.vm.ctx.Log.Debug("proposervm: GetProposedHeight called")

	height, err := p.vm.ctx.ValidatorState.GetMinimumHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't get minimum height %w", err)
	}

	return connect.NewResponse(&pb.GetProposedHeightReply{
		Height: height,
	}), nil
}
