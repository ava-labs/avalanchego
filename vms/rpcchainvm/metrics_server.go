// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	vmpb "github.com/chain4travel/caminogo/proto/pb/vm"
)

func (vm *VMServer) Gather(context.Context, *emptypb.Empty) (*vmpb.GatherResponse, error) {
	mfs, err := vm.ctx.Metrics.Gather()
	return &vmpb.GatherResponse{MetricFamilies: mfs}, err
}
