// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// filterAPI is a replacement for a [filters.FilterAPI] which applies
// [Config] for any call to [filters.FilterAPI.GetLogs].
type filterAPI struct {
	*filters.FilterAPI
	b *backend
}

// GetLogs overrides [filters.FilterAPI.GetLogs] to adjust the filter criteria
// if necessary based on the [Config]. Only invalid criteria with respect to the
// config will error before being sent to the inner call.
func (api *filterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*types.Log, error) {
	newCrit, err := api.updateFilterCriteria(crit)
	if err != nil {
		return nil, err
	}
	return api.FilterAPI.GetLogs(ctx, newCrit)
}

func (api *filterAPI) updateFilterCriteria(crit filters.FilterCriteria) (filters.FilterCriteria, error) {
	if crit.BlockHash != nil {
		return crit, nil
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
		return crit, nil //nolint:nilerr // [filters.FilterAPI.GetLogs] will handle the error
	}
	resolvedEnd, err := blocks.ResolveRPCNumber(api.b, end)
	if err != nil {
		return crit, nil //nolint:nilerr // [filters.FilterAPI.GetLogs] will handle the error
	}

	if resolvedEnd < resolvedBegin {
		return crit, nil
	}

	if maxBlocks := api.b.config.MaxBlocksPerRequest; maxBlocks > 0 && resolvedEnd-resolvedBegin >= maxBlocks {
		return crit, fmt.Errorf(
			"requested too many blocks from %d to %d, maximum is set to %d",
			resolvedBegin,
			resolvedEnd,
			api.b.config.MaxBlocksPerRequest,
		)
	}

	// Uses [Config.ResolvePendingToLastExecuted] and avoids unintuitive geth handling
	crit.FromBlock = new(big.Int).SetUint64(resolvedBegin)
	crit.ToBlock = new(big.Int).SetUint64(resolvedEnd)

	return crit, nil
}
