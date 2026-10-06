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

// filterAPI is a replacement for a [filters.FilterAPI] which applies [Config]
// for any call to [filters.FilterAPI.GetLogs]. This does NOT apply to any
// other methods in [filters.FilterAPI].
type filterAPI struct {
	*filters.FilterAPI
	b *backend
}

// GetLogs overrides [filters.FilterAPI.GetLogs] to adjust the filter criteria
// if necessary based on the [Config]. Only invalid criteria with respect to the
// config will error before being sent to the inner call.
func (api *filterAPI) GetLogs(ctx context.Context, crit filters.FilterCriteria) ([]*types.Log, error) {
	if crit.BlockHash != nil {
		// crit.FromBlock and crit.ToBlock are ignored
		return api.FilterAPI.GetLogs(ctx, crit)
	}

	resolvedBegin, err := api.resolve(crit.FromBlock)
	if err != nil {
		// start block isn't found, no hope of finding logs
		return nil, nil //nolint:nilerr // match libevm behavior for unknown blocks
	}

	resolvedEnd, err := api.resolve(crit.ToBlock)
	if err != nil {
		// libevm will iterate through all blocks through to the end searching
		// for logs. To save this iteration, we can cap the end block to the last
		// block possible to have logs, but still >= resolvedBegin
		resolvedEnd = api.b.LastAccepted().Height()
	}

	// libevm would resolve this differently
	crit.FromBlock = new(big.Int).SetUint64(resolvedBegin)
	crit.ToBlock = new(big.Int).SetUint64(resolvedEnd)
	if resolvedEnd < resolvedBegin {
		return api.FilterAPI.GetLogs(ctx, crit) // allow libevm to handle error
	}

	if maxBlocks := api.b.config.MaxBlocksPerRequest; maxBlocks > 0 && resolvedEnd-resolvedBegin >= maxBlocks {
		return nil, fmt.Errorf(
			"requested too many blocks from %d to %d, maximum is set to %d",
			resolvedBegin,
			resolvedEnd,
			api.b.config.MaxBlocksPerRequest,
		)
	}

	return api.FilterAPI.GetLogs(ctx, crit)
}

// resolve attempts to determine the block number corresponding to the given
// big.Int. Any error indicates the block can't be found.
func (api *filterAPI) resolve(number *big.Int) (uint64, error) {
	block := rpc.LatestBlockNumber
	if number != nil {
		block = rpc.BlockNumber(number.Int64())
	}

	return blocks.ResolveRPCNumber(api.b, block)
}
