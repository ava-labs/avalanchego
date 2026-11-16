// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package long_running

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

// Set this to a higher value so we don't overload lagging nodes with replication requests
const longRunningMaxRoundWindow = 100

type LongRunningInMemoryNetwork struct {
	t    *testing.T
	lock sync.Mutex
	*testutil.BasicInMemoryNetwork

	instances []*LongRunningNode
	stopped   atomic.Bool
}

func NewDefaultLongRunningNetwork(t *testing.T, numNodes int) *LongRunningInMemoryNetwork {
	nodes := make([]common.NodeID, numNodes)
	for i := range numNodes {
		nodes[i] = testutil.GenerateNodeID(t)
	}

	instances := make([]*LongRunningNode, 0, numNodes)
	net := testutil.NewBasicInMemoryNetwork(t, nodes)
	for _, nodeID := range nodes {
		node := NewLongRunningNode(t, nodeID, net, longRunningNodeConfig{})
		instances = append(instances, node)
		net.AddNode(node.BasicNode)
	}

	return &LongRunningInMemoryNetwork{
		BasicInMemoryNetwork: net,
		instances:            instances,
		t:                    t,
	}
}

func (n *LongRunningInMemoryNetwork) GetInstance(index int) *LongRunningNode {
	return n.instances[index]
}

func (n *LongRunningInMemoryNetwork) StartInstances() {
	n.BasicInMemoryNetwork.StartInstances()

	// start time updater
	amount := simplex.DefaultEmptyVoteRebroadcastTimeout / 5
	go n.UpdateTime(100*time.Millisecond, amount)
}

func (n *LongRunningInMemoryNetwork) UpdateTime(frequency time.Duration, increment time.Duration) {
	for !n.stopped.Load() {
		n.AdvanceTime(increment)
		time.Sleep(frequency)
	}
}

func (n *LongRunningInMemoryNetwork) CrashNodes(nodeIndexes ...uint64) {
	for _, idx := range nodeIndexes {
		instance := n.instances[idx]
		instance.Stop()
	}
}

func (n *LongRunningInMemoryNetwork) StartNodes(nodeIndexes ...uint64) {
	for _, idx := range nodeIndexes {
		n.lock.Lock()
		instance := n.instances[idx]

		nodeID := instance.E.ID
		bb := instance.bb
		clonedWal := instance.wal.Clone()
		clonedStorage := instance.Storage.Clone()

		config := longRunningNodeConfig{
			bb:      bb,
			wal:     clonedWal,
			storage: clonedStorage,
			logger:  instance.logger,
		}
		newNode := NewLongRunningNode(n.t, nodeID, n.BasicInMemoryNetwork, config)

		n.instances[idx] = newNode
		n.ReplaceNode(newNode.BasicNode)

		n.lock.Unlock()
		newNode.Start()
	}
}

func (n *LongRunningInMemoryNetwork) NoMoreBlocks() {
	for _, instance := range n.instances {
		instance.bb.TriggerBlockShouldNotBeBuilt()
	}
}

func (n *LongRunningInMemoryNetwork) ContinueBlocks() {
	for _, instance := range n.instances {
		instance.bb.TriggerBlockShouldBeBuilt()
	}
}

func (n *LongRunningInMemoryNetwork) WaitForNodesToEnterRound(round uint64, nodeIndexes ...uint64) {
	// check if nodeIndexes have length 0
	if len(nodeIndexes) == 0 {
		for _, instance := range n.instances {
			testutil.WaitToEnterRoundWithTimeout(n.t, instance.E, round, 3*time.Second)
		}
	}

	for _, idx := range nodeIndexes {
		testutil.WaitToEnterRoundWithTimeout(n.t, n.instances[idx].E, round, 3*time.Second)
	}
}

func (n *LongRunningInMemoryNetwork) DisconnectNodes(nodeIndexes ...uint64) {
	for _, idx := range nodeIndexes {
		nodeID := n.instances[idx].E.ID
		n.Disconnect(nodeID)
	}
}

func (n *LongRunningInMemoryNetwork) ConnectNodes(nodeIndexes ...uint64) {
	for _, idx := range nodeIndexes {
		nodeID := n.instances[idx].E.ID
		n.Connect(nodeID)
	}
}

func (n *LongRunningInMemoryNetwork) waitUntilAllRoundsEqual() {
	maxWait := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-maxWait:
			n.t.Fatal("timed out waiting for all nodes to have the same round")
		case <-ticker.C:
			rounds := make(map[uint64]struct{})
			for _, instance := range n.instances {
				rounds[instance.E.Metadata().Round] = struct{}{}
			}
			if len(rounds) == 1 {
				return
			}
		}
	}
}

// StopAndAssert stops all nodes and asserts their storage and WALs are consistent.
// If tailingMessages is true, it will also assert that no extra messages are being sent
// after telling the network no more blocks should be built.
// TailingMessages is useful for debugging, but is currently flakey because NoMoreBlocks does not stop block building
// atomically for all nodes in the network. Therefore, some nodes may timeout on a block they think should be built, and keep resending empty votes.
func (n *LongRunningInMemoryNetwork) StopAndAssert(tailingMessages bool) {
	n.NoMoreBlocks()

	// ensures all nodes have the same round before checking storage/WAL consistency
	n.waitUntilAllRoundsEqual()

	// check all the nodes have the same wal, storage, etc
	for i, instance := range n.instances {
		instance.wal.AssertHealthy(instance.E.BlockDeserializer, instance.E.QCDeserializer)
		if i != 0 {
			require.NoError(n.t, instance.Storage.Compare(n.instances[0].Storage), "node %d storage does not match node 0 storage", i)
		}
		instance.Stop()
	}

	// print summary of messages sent
	for _, instance := range n.instances {
		instance.PrintMessageTypesSent()
	}

	if tailingMessages {
		// assert no extra messages/requests are being sent
		time.Sleep(3 * time.Second)
		for _, instance := range n.instances {
			instance.ResetMessageTypesSent()
		}
		time.Sleep(3 * time.Second)
		for _, instance := range n.instances {
			require.Empty(n.t, instance.GetMessageTypesSent(), "expected no messages to be sent after telling the network no more blocks should be built, node ID: %s\n msg %+v", instance.E.ID.String(), instance.GetMessageTypesSent())
		}
	}

	n.stopped.Store(true)
}
