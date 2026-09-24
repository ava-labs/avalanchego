// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

type filterAPI struct {
	*filters.FilterAPI
	b                   *backend
	maxBlocksPerRequest int64
}

// GetLogs overrides [filters.FilterAPI.GetLogs] to reject block ranges larger
// than the configured maximum before asking libevm to scan them.
func (api *filterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*types.Log, error) {
	if api.maxBlocksPerRequest == 0 || crit.BlockHash != nil {
		return api.FilterAPI.GetLogs(ctx, crit)
	}

	begin := rpc.LatestBlockNumber
	if crit.FromBlock != nil {
		begin = rpc.BlockNumber(crit.FromBlock.Int64())
	}
	end := rpc.LatestBlockNumber
	if crit.ToBlock != nil {
		end = rpc.BlockNumber(crit.ToBlock.Int64())
	}

	resolvedBegin, err := blocks.ResolveRPCNumber(api.b, begin)
	if err != nil {
		return nil, fmt.Errorf("resolving beginning block: %w", err)
	}
	resolvedEnd, err := blocks.ResolveRPCNumber(api.b, end)
	if err != nil {
		return nil, fmt.Errorf("resolving ending block: %w", err)
	}

	if resolvedEnd < resolvedBegin {
		return nil, fmt.Errorf(
			"ending block %d is before beginning block %d",
			resolvedEnd,
			resolvedBegin,
		)
	}

	if int64(resolvedEnd-resolvedBegin) >= api.maxBlocksPerRequest { //#nosec G115 -- won't overflow for a while
		return nil, fmt.Errorf(
			"requested too many blocks from %d to %d, maximum is set to %d",
			resolvedBegin,
			resolvedEnd,
			api.maxBlocksPerRequest,
		)
	}
	return api.FilterAPI.GetLogs(ctx, crit)
}
