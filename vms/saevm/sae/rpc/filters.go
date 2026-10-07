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

	start, foundStart := api.resolve(crit.FromBlock)
	end, foundEnd := api.resolve(crit.ToBlock)
	switch {
	case !foundStart && !foundEnd:
		// With neither end found, one can assume there are no intermediate
		// known blocks either.
		return nil, nil //nolint:nilerr // match libevm behavior for unknown blocks
	case !foundStart:
		// There MAY be intermediate blocks available (e.g. statesync).
		// crit.FromBlock MUST be non-negative, otherwise it would have been found.
		start = crit.FromBlock.Uint64()
	case !foundEnd:
		// MUST be >= start, but we won't find unexecuted logs
		end = api.b.LastAccepted().Height()
	}

	// In all cases, crit MUST be adjusted to reflect actual block numbers.
	crit.FromBlock = new(big.Int).SetUint64(start)
	crit.ToBlock = new(big.Int).SetUint64(end)
	if end < start {
		return api.FilterAPI.GetLogs(ctx, crit) // allow libevm to handle error
	}

	if maxBlocks := api.b.config.MaxBlocksPerRequest; maxBlocks > 0 && end-start >= maxBlocks {
		return nil, fmt.Errorf(
			"requested too many blocks from %d to %d, maximum is set to %d",
			start,
			end,
			api.b.config.MaxBlocksPerRequest,
		)
	}

	return api.FilterAPI.GetLogs(ctx, crit)
}

// resolve attempts to determine the block number corresponding to the given
// big.Int. Returning false indicates that the number supplied was non-negative
// AND cannot be found.
func (api *filterAPI) resolve(number *big.Int) (uint64, bool) {
	block := rpc.LatestBlockNumber
	if number != nil {
		block = rpc.BlockNumber(number.Int64())
	}

	n, err := blocks.ResolveRPCNumber(api.b, block)
	if err != nil {
		return 0, false
	}
	return n, true
}
