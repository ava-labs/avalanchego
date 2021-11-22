// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/vmproto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func (vm *VMServer) Gather(context.Context, *emptypb.Empty) (*vmproto.GatherResponse, error) {
	mfs, err := vm.ctx.Metrics.Gather()
	return &vmproto.GatherResponse{MetricFamilies: mfs}, err
}
