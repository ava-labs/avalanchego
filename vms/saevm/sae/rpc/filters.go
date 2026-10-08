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
	if !foundStart {
		// The first block read will fail. Might as well return now.
		return []*types.Log{}, nil
	}

	end, foundEnd := api.resolve(crit.ToBlock)
	if !foundEnd {
		// All special numbers resolve, so this must be a future block.
		// MUST be >= start, but we won't find unexecuted logs
		end = api.b.LastAccepted().Height()
	}

	if end < start {
		// Invalid input will be handled by libevm. A short proof that this doesn't
		// escape the max blocks per request limit is below.
		// - End wasn't found
		//   - If start was a special value, it was assumed to be within
		//     [Config.MaxBlocksPerRequest]. It will read until the first unexecuted block.
		//   - If start was not a special value, then it was a future block (> last accepted),
		//     in which case it couldn't have been found. Contradiction.
		// - End was found. Then it resolved to a value less than the provided start.
		//   In all cases, this is user error, and libevm will handle accordingly.
		return api.FilterAPI.GetLogs(ctx, crit)
	}

	if maxBlocks := api.b.config.MaxBlocksPerRequest; maxBlocks > 0 && end-start >= maxBlocks {
		return nil, fmt.Errorf(
			"requested too many blocks from %d to %d, maximum is set to %d",
			start,
			end,
			api.b.config.MaxBlocksPerRequest,
		)
	}

	crit.FromBlock = new(big.Int).SetUint64(start)
	crit.ToBlock = new(big.Int).SetUint64(end)
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
