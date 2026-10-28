// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// PredicateResults returns the parsed predicate results from predicateBytes,
// caching them on h. If predicateBytes is empty, or cannot be parsed, nil is
// returned. The same predicateBytes MUST be supplied for each call to a given
// header.
func PredicateResults(h *ethtypes.Header, predicateBytes []byte) message.BlockResults {
	if len(predicateBytes) == 0 {
		return nil
	}

	cache := &GetHeaderExtra(h).predicateResults
	if results := cache.Load(); results != nil {
		return *results
	}

	// results were already verified for canonical execution
	results, _ := message.ParseBlockResults(predicateBytes)
	cache.Store(&results)
	return results
}
