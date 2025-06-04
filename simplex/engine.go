// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"context"
	"simplex"
	"sync"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ common.Engine = (*Engine)(nil)

type Engine struct {
	common.Handler
	health.Checker

	log logging.Logger
	vm  block.ChainVM

	// simplex digest to vm digest cache
	lock        sync.Mutex
	digestCache map[simplex.Digest]ids.ID
}

func NewEngine(
	checker health.Checker,
	log logging.Logger,
) *Engine {
	log.Debug("Creating new simplex engine wooooooo")
	return &Engine{
		Checker: checker,
		log:     log,
	}
}

func (e *Engine) Simplex(nodeID ids.NodeID, _ *p2p.Simplex) error {
	e.log.Debug("Simplex request in engine from %s", zap.Stringer("nodeID", nodeID))
	return nil
}

func (*Engine) Start(_ context.Context, _ uint32) error {
	return nil
}

func (e *Engine) removeDigestToIDMapping(digest simplex.Digest) {
	e.lock.Lock()
	defer e.lock.Unlock()

	delete(e.digestCache, digest)
}

func (e *Engine) observeDigestToIDMapping(digest simplex.Digest, blockID ids.ID) {
	e.lock.Lock()
	defer e.lock.Unlock()

	// the block ID is input to the entire simplex block, therefore
	// if the leader equivocates the simplex digest will be different.
	e.digestCache[digest] = blockID
}
