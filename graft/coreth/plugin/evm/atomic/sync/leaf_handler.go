// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"github.com/MetalBlockchain/libevm/metrics"
	"github.com/MetalBlockchain/libevm/triedb"

	"github.com/MetalBlockchain/metalgo/codec"
	"github.com/MetalBlockchain/metalgo/graft/evm/message"
	"github.com/MetalBlockchain/metalgo/graft/evm/sync/handlers"
	"github.com/MetalBlockchain/metalgo/graft/evm/sync/handlers/stats"
	"github.com/MetalBlockchain/metalgo/ids"
)

var (
	_ handlers.LeafRequestHandler = (*uninitializedHandler)(nil)

	errUninitialized = errors.New("uninitialized handler")
)

type uninitializedHandler struct{}

func (*uninitializedHandler) OnLeafsRequest(context.Context, ids.NodeID, uint32, message.LeafsRequest) ([]byte, error) {
	return nil, errUninitialized
}

// atomicLeafHandler is a wrapper around handlers.LeafRequestHandler that allows for initialization after creation
type leafHandler struct {
	handlers.LeafRequestHandler
}

// NewAtomicLeafHandler returns a new uninitialized leafHandler that can be later initialized
func NewLeafHandler() *leafHandler {
	return &leafHandler{
		LeafRequestHandler: &uninitializedHandler{},
	}
}

// Initialize initializes the atomicLeafHandler with the provided atomicTrieDB, trieKeyLength, and networkCodec
func (a *leafHandler) Initialize(atomicTrieDB *triedb.Database, trieKeyLength int, networkCodec codec.Manager) {
	handlerStats := stats.GetOrRegisterHandlerStats(metrics.Enabled)
	a.LeafRequestHandler = handlers.NewLeafsRequestHandler(atomicTrieDB, trieKeyLength, nil, networkCodec, handlerStats)
}
