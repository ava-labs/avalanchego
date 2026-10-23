// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/filters"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// A filterBackend is a [backend] that hides accepted blocks until they are
// executed. Headers are written on acceptance but logs only on execution, and
// libevm's filter system errors on any header whose logs are missing.
type filterBackend struct {
	*backend
}

func (b filterBackend) HeaderByNumber(ctx context.Context, n rpc.BlockNumber) (*types.Header, error) {
	return b.executed(b.backend.HeaderByNumber(ctx, n))
}

func (b filterBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.executed(b.backend.HeaderByHash(ctx, hash))
}

func (b filterBackend) executed(h *types.Header, err error) (*types.Header, error) {
	if err != nil || h == nil || h.Number.Uint64() > b.LastExecuted().Height() {
		return nil, err
	}
	return h, nil
}

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

	// [filters.FilterCriteria.UnmarshalJSON] guarantees that FromBlock and ToBlock
	// are both valid [rpc.BlockNumber] values.

	start, foundStart := api.resolve(crit.FromBlock)
	if !foundStart {
		// All special numbers resolve, so this must be a future block.
		// This could at most fetch logs from this block (if it has been
		// executed after the above call) until a missing block is hit.
		// This is assumed to be within [Config.MaxBlocksPerRequest].
		return api.FilterAPI.GetLogs(ctx, crit)
	}

	end, foundEnd := api.resolve(crit.ToBlock)
	if !foundEnd {
		// All special numbers resolve, so this must be a future block. Cap at
		// the last-accepted block, where libevm would stop anyway.
		end = api.b.LastAccepted().Height()
	}

	if end < start {
		// A short proof that this doesn't escape [Config.MaxBlocksPerRequest]:
		//
		// Assume ToBlock wasn't found (i.e. `!foundEnd`). `start` was found, otherwise we would
		// have returned earlier. Since `end` is capped at the last accepted block, and it
		// was determined after `start` (the last accepted block could have updated), we know
		// `start <= end`. Thus, this `if` statement could not be hit (contradiction).
		//
		// Assume ToBlock was found. If ToBlock was a special number, then `start` must be
		// at minimum the last finalized block, which we assume is within
		// [Config.MaxBlocksPerRequest] from the last accepted block, which libevm will bound to.
		// If ToBlock wasn't a special number, then this is user error, and will be handled
		// by libevm.
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
