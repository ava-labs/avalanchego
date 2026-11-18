// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/simplex/common"
	"github.com/stretchr/testify/require"
)

type BasicInMemoryNetwork struct {
	t            *testing.T
	nodes        []common.NodeID
	nodeWeights  common.Nodes
	lock         sync.RWMutex
	disconnected map[string]struct{}
	instances    []*BasicNode
	// pendingMessages counts messages enqueued to any instance that have not finished being handled.
	// A node increments it for every message it sends before decrementing it for the message it is
	// handling, so it only reaches zero once no message is queued or being handled anywhere.
	pendingMessages atomic.Int64
}

func NewBasicInMemoryNetwork(t *testing.T, nodes common.NodeIDs) *BasicInMemoryNetwork {
	nodeWeights := nodes.EqualWeightedNodes()
	common.SortNodes(nodeWeights)
	return &BasicInMemoryNetwork{
		t:            t,
		nodeWeights:  nodeWeights,
		nodes:        nodes,
		disconnected: make(map[string]struct{}),
		instances:    make([]*BasicNode, 0),
	}
}

type TestNetworkCommunication interface {
	common.Communication
	SetFilter(filter MessageFilter)
}

func (b *BasicInMemoryNetwork) SetNodeMessageFilter(node common.NodeID, filter MessageFilter) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	for _, instance := range b.instances {
		if !instance.E.ID.Equals(node) {
			continue
		}
		comm, ok := instance.E.Comm.(TestNetworkCommunication)
		if !ok {
			continue
		}
		comm.SetFilter(filter)
	}
}

func (b *BasicInMemoryNetwork) SetAllNodesMessageFilter(filter MessageFilter) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	for _, instance := range b.instances {
		comm, ok := instance.E.Comm.(TestNetworkCommunication)
		if !ok {
			continue
		}
		comm.SetFilter(filter)
	}
}

func (b *BasicInMemoryNetwork) IsDisconnected(node common.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	_, ok := b.disconnected[string(node)]
	return ok
}

func (b *BasicInMemoryNetwork) Connect(node common.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()

	delete(b.disconnected, string(node))
}

func (b *BasicInMemoryNetwork) Disconnect(node common.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.disconnected[string(node)] = struct{}{}
}

func (b *BasicInMemoryNetwork) AdvanceTime(increment time.Duration) {
	// iterate a snapshot instead of holding the lock, since advancing time can
	// send messages, which acquires the lock again
	for _, instance := range b.GetInstances() {
		instance.AdvanceTime(increment)
	}
}

// StartInstances starts all instances in the network.
// The first one is typically the leader, so we make sure to start it last.
func (b *BasicInMemoryNetwork) StartInstances() {
	b.lock.RLock()
	defer b.lock.RUnlock()

	require.Equal(b.t, len(b.nodeWeights), len(b.instances))

	for i := len(b.nodeWeights) - 1; i >= 0; i-- {
		b.instances[i].Start()
	}
}

func (b *BasicInMemoryNetwork) StopInstances() {
	b.lock.RLock()
	defer b.lock.RUnlock()

	for _, instance := range b.instances {
		instance.Stop()
	}
}

func (b *BasicInMemoryNetwork) GetInstances() []*BasicNode {
	b.lock.RLock()
	defer b.lock.RUnlock()

	nodes := make([]*BasicNode, len(b.instances))
	copy(nodes, b.instances)
	return nodes
}

func (b *BasicInMemoryNetwork) ReplaceNode(node *BasicNode) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for i, instance := range b.instances {
		if bytes.Equal(instance.E.ID, node.E.ID) {
			// Replace the instance in the slice
			node.pendingMessages = &b.pendingMessages
			b.instances[i] = node
			return
		}
	}
}

func (b *BasicInMemoryNetwork) AddNode(node *BasicNode) {
	b.lock.Lock()
	defer b.lock.Unlock()

	allowed := false
	for _, nodeWeight := range b.nodeWeights {
		if bytes.Equal(nodeWeight.Id, node.E.ID) {
			allowed = true
			break
		}
	}
	require.True(node.t, allowed, "node must be declared before adding")
	node.pendingMessages = &b.pendingMessages
	b.instances = append(b.instances, node)
}

// WaitForQuiescence waits until every message sent between the instances has been handled,
// including the messages sent while handling them.
func (b *BasicInMemoryNetwork) WaitForQuiescence() {
	require.Eventually(b.t, func() bool {
		return b.pendingMessages.Load() == 0
	}, 30*time.Second, 10*time.Millisecond, "messages are still pending between the instances")
}

func (b *BasicInMemoryNetwork) RemoveNode(node *BasicNode) {
	b.lock.Lock()
	defer b.lock.Unlock()

	for i, instance := range b.instances {
		if instance == node {
			// Remove the instance from the slice
			b.instances = append(b.instances[:i], b.instances[i+1:]...)
			return
		}
	}
}
