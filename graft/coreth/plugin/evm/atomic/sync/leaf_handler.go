// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/coreth/sync/handlers"
	"github.com/ava-labs/coreth/sync/handlers/stats"

	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"
)

// leafHandler is a wrapper around handlers.LeafRequestHandler that allows for initialization after creation
type leafHandler struct {
	handlers.LeafRequestHandler
}

// Initialize initializes the leafHandler with the provided atomicTrieDB, trieKeyLength, and networkCodec
func NewLeafHandler(atomicTrieDB *triedb.Database, trieKeyLength int, networkCodec codec.Manager) *leafHandler {
	handlerStats := stats.GetOrRegisterHandlerStats(metrics.Enabled)
	return &leafHandler{
		LeafRequestHandler: handlers.NewLeafsRequestHandler(atomicTrieDB, trieKeyLength, nil, networkCodec, handlerStats),
	}
}
