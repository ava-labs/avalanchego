// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

type connections struct {
	lock  sync.RWMutex
	nodes map[ids.NodeID]*version.Application
}

func newConnections() *connections {
	return &connections{
		nodes: make(map[ids.NodeID]*version.Application),
	}
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

func (c *connections) reconnect(ctx context.Context, connector validators.Connector) error {
	c.lock.RLock()
	defer c.lock.RUnlock()

	for nodeID, v := range c.nodes {
		if err := connector.Connected(ctx, nodeID, v); err != nil {
			return err
		}
	}
	return nil
}

var _ common.AppSender = (*sender)(nil)

//nolint:unused // False positive
type request struct {
	nodeID    ids.NodeID
	requestID uint32
}

type requests struct {
	lock sync.Mutex
	set  set.Set[request]
}

func (r *requests) add(nodeIDs set.Set[ids.NodeID], requestID uint32) {
	r.lock.Lock()
	defer r.lock.Unlock()

	for nodeID := range nodeIDs {
		r.set.Add(request{nodeID, requestID})
	}
}

func (r *requests) remove(nodeID ids.NodeID, requestID uint32) bool {
	r.lock.Lock()
	defer r.lock.Unlock()

	req := request{nodeID, requestID}
	had := r.set.Contains(req)
	r.set.Remove(req)
	return had
}

type sender struct {
	common.AppSender

	requests *requests
}

func (s *sender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) error {
	s.requests.add(nodeIDs, requestID)
	return s.AppSender.SendAppRequest(ctx, nodeIDs, requestID, appRequestBytes)
}

func (vm *VM) Connected(ctx context.Context, nodeID ids.NodeID, version *version.Application) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.connections.add(nodeID, version)
	return vm.current.chain.Connected(ctx, nodeID, version)
}

func (vm *VM) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	vm.connections.remove(nodeID)
	return vm.current.chain.Disconnected(ctx, nodeID)
}

func (vm *VM) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	if !vm.current.requests.remove(nodeID, requestID) {
		return nil
	}
	return vm.current.chain.AppRequestFailed(ctx, nodeID, requestID, appErr)
}

func (vm *VM) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	vm.transitionLock.RLock()
	defer vm.transitionLock.RUnlock()

	if !vm.current.requests.remove(nodeID, requestID) {
		return nil
	}
	return vm.current.chain.AppResponse(ctx, nodeID, requestID, response)
}
