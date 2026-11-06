// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"github.com/ava-labs/avalanchego/graft/evm/predicate"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// PredicateResults returns the parsed predicate results from the header's extra
// data. If the extra data does not contain predicate results, or cannot be
// parsed, nil is returned. The same rules MUST be supplied for each call to a
// given header.
func PredicateResults(h *ethtypes.Header, predicateBytes []byte) predicate.BlockResults {
	if len(predicateBytes) == 0 {
		return nil
	}

	cache := &GetHeaderExtra(h).predicateResults
	if results := cache.Load(); results != nil {
		return *results
	}

	// results were already verified for canonical execution
	results, _ := predicate.ParseBlockResults(predicateBytes)
	cache.Store(&results)
	return results
}
