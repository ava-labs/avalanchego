// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"errors"
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
	newCrit, err := api.updateFilterCriteria(crit)
	if err != nil {
		return nil, err
	}
	return api.FilterAPI.GetLogs(ctx, newCrit)
}

func (api *filterAPI) updateFilterCriteria(crit filters.FilterCriteria) (filters.FilterCriteria, error) {
	if crit.BlockHash != nil {
		// crit.FromBlock and crit.ToBlock are ignored
		return crit, nil
	}

	resolvedBegin, ok := api.resolve(crit.FromBlock)
	if !ok {
		return crit, nil
	}
	resolvedEnd, ok := api.resolve(crit.ToBlock)
	if !ok {
		return crit, nil
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

	// libevm MAY resolve this differently, but we should use the node's config.
	crit.FromBlock = new(big.Int).SetUint64(resolvedBegin)
	crit.ToBlock = new(big.Int).SetUint64(resolvedEnd)

	return crit, nil
}

// resolve attempts to determine the block number corresponding to the given
// big.Int. Any errors encountered during the resolution can still be handled
// by [filters.FilterAPI.GetLogs].
func (api *filterAPI) resolve(number *big.Int) (uint64, bool) {
	block := rpc.LatestBlockNumber
	if number != nil {
		block = rpc.BlockNumber(number.Int64())
	}

	resolved, err := blocks.ResolveRPCNumber(api.b, block)
	switch {
	case errors.Is(err, blocks.ErrFutureBlockNotResolved):
		// libevm would otherwise scan through the provided block number.
		return api.b.LastAccepted().Height(), true
	case err != nil:
		return 0, false
	}

	return resolved, true
}
