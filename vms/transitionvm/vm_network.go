// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/version"
)

type connections struct {
	lock  sync.Mutex
	nodes map[ids.NodeID]*version.Application
}

func (c *connections) add(nodeID ids.NodeID, v *version.Application) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.nodes[nodeID] = v
}

func (c *connections) remove(nodeID ids.NodeID) {
	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.nodes, nodeID)
}

func (v *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	v.current.connections.add(nodeID, version)
	return v.current.chain.Connected(ctx, nodeID, version)
}

func (v *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	v.current.connections.remove(nodeID)
	return v.current.chain.Disconnected(ctx, nodeID)
}

func (v *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	if !v.current.requests.remove(nodeID, requestID) {
		return nil
	}
	return v.current.chain.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (v *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	v.transitionLock.RLock()
	defer v.transitionLock.RUnlock()

	if !v.current.requests.remove(nodeID, requestID) {
		return nil
	}
	return v.current.chain.AppResponse(ctx, nodeID, requestID, response)
}
