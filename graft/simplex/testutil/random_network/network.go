// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random_network

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Network struct {
	*testutil.BasicInMemoryNetwork
	logger *testutil.TestLogger
	t      *testing.T

	lock       sync.Mutex
	nodes      []*Node
	numNodes   uint64
	randomness *rand.Rand
	config     *FuzzConfig

	// tx stats
	allIssuedTxs int
}

func NewNetwork(config *FuzzConfig, t *testing.T) *Network {
	logger := CreateNetworkLogger(t, config)
	logger.Info("Creating new network with random seed", zap.Int64("seed", config.RandomSeed))
	r := rand.New(rand.NewSource(config.RandomSeed))

	numNodes := r.Intn(config.MaxNodes-config.MinNodes+1) + config.MinNodes
	nodeIds := make([]common.NodeID, numNodes)
	for i := range numNodes {
		nodeIds[i] = []byte{byte(i)}
	}

	nodes := make([]*Node, numNodes)

	logger.Info("Initiating network with nodes", zap.Int("num nodes", numNodes))
	basicNetwork := testutil.NewBasicInMemoryNetwork(t, nodeIds)

	for i := range numNodes {
		node := NewNode(t, nodeIds[i], basicNetwork, config, randomNodeConfig{})
		basicNetwork.AddNode(node.BasicNode)
		nodes[i] = node
	}

	return &Network{
		BasicInMemoryNetwork: basicNetwork,
		nodes:                nodes,
		randomness:           r,
		logger:               logger,
		t:                    t,
		config:               config,
		numNodes:             uint64(numNodes),
	}
}

func (n *Network) StartInstances() {
	panic("Call Run() Instead")
}

func (n *Network) Run() {
	n.BasicInMemoryNetwork.StartInstances()
	defer n.StopInstances()

	targetHeight := uint64(n.config.NumFinalizedBlocks)

	for {
		n.crashAndRecoverNodes()
		txs := n.issueTxs()

		maxHeight := n.getMaxHeight()
		minHeight := n.getMinHeight()
		n.logger.Info("Issued Transactions", zap.Int("count", len(txs)), zap.Uint64("min height", minHeight), zap.Uint64("max height", maxHeight))
		n.waitForTxAcceptance(txs)

		maxHeight = n.getMaxHeight()
		minHeight = n.getMinHeight()
		n.logger.Info("All issued transactions accepted", zap.Int("count", len(txs)), zap.Uint64("min height", minHeight), zap.Uint64("max height", maxHeight))
		// get the max height and ensure all nodes recover to that height
		n.recoverToHeight(n.getMaxHeight())

		if minHeight >= targetHeight {
			n.logger.Info("Reached target height", zap.Uint64("targetHeight", targetHeight), zap.Uint64("minHeight", minHeight))
			break
		}
	}

	// if we have gotten this far, the test has succeeded so we can clear the log directory
	if err := clearLogDirectory(n.config.LogDirectory); err != nil {
		n.t.Fatal(err)
	}
}

func (n *Network) getMinHeightNodeID() common.NodeID {
	minHeight := n.nodes[0].storage.NumBlocks()
	minHeightNodeID := n.nodes[0].E.ID

	for _, node := range n.nodes[1:] {
		height := node.storage.NumBlocks()

		if height < minHeight {
			minHeight = height
			minHeightNodeID = node.E.ID
		}
	}

	return minHeightNodeID
}

func (n *Network) recoverToHeight(height uint64) {
	for n.getMinHeight() < height {
		n.logger.Debug("Advancing network time", zap.Uint64("num crashed nodes", n.numCrashedNodes()),
			zap.Uint64("min height", n.getMinHeight()),
			zap.Uint64("max height", n.getMaxHeight()),
			zap.Stringer("Smallest node ID", n.getMinHeightNodeID()),
			zap.Uint64("target height", height),
		)
		for i, node := range n.nodes {
			isCrashed := node.isCrashed.Load()
			if isCrashed {
				// randomly decide to recover based on NodeRecoverPercentage
				if n.randomness.Float64() < n.config.NodeRecoverProbability {
					n.logger.Debug("Recovering node", zap.Stringer("nodeID", node.E.ID))
					n.startNode(i)
				}
			}
		}

		n.lock.Lock()
		n.AdvanceTime(n.config.AdvanceTimeTickAmount)
		n.lock.Unlock()
	}

}

func (n *Network) issueTxs() []*TX {
	n.lock.Lock()
	defer n.lock.Unlock()

	numTxs := n.randomness.Intn(n.config.MaxTxsPerIssue-n.config.MinTxsPerIssue+1) + n.config.MinTxsPerIssue // randomize between min and max inclusive
	txs := make([]*TX, 0, numTxs)

	for range numTxs {
		tx := CreateNewTX()
		txs = append(txs, tx)
		n.allIssuedTxs++
	}

	// first add to all mempools
	for _, node := range n.nodes {
		node.mempool.AddPendingTXs(txs...)
	}

	// then notify all mempools that new txs are ready
	for _, node := range n.nodes {
		node.mempool.NotifyTxsReady()
	}

	return txs
}

func (n *Network) waitForTxAcceptance(txs []*TX) {
	for {
		allAccepted := true
		for _, node := range n.nodes {
			if node.isCrashed.Load() {
				continue
			}
			if accepted := node.areTxsAccepted(txs); !accepted {
				node.mempool.lock.Lock()
				n.logger.Debug("Not all txs accepted yet by node", zap.Stringer("nodeID", node.E.ID), zap.Int("unaccepted txs in mempool", len(node.mempool.unacceptedTxs)))
				node.mempool.lock.Unlock()
				allAccepted = false
			}
		}

		if allAccepted {
			return
		}

		n.lock.Lock()
		n.logger.Debug("Advancing network time to wait for tx acceptance", zap.Uint64("num crashed nodes", n.numCrashedNodes()))
		n.AdvanceTime(n.config.AdvanceTimeTickAmount)
		n.lock.Unlock()

		time.Sleep(20 * time.Millisecond)
	}
}

func (n *Network) numCrashedNodes() uint64 {
	numCrashed := 0
	for _, node := range n.nodes {
		if node.isCrashed.Load() {
			numCrashed++
		}
	}
	return uint64(numCrashed)
}

func (n *Network) crashAndRecoverNodes() {
	if n.config.NodeCrashProbability == 0 {
		return
	}

	f := (int(n.numNodes) - 1) / 3

	if f == 0 {
		n.logger.Info("Not enough nodes for crash testing", zap.Uint64("numNodes", n.numNodes))
		return
	}

	crashedNodes := []common.NodeID{}
	recoveredNodes := []common.NodeID{}
	maxLeftToCrash := f - int(n.numCrashedNodes())
	// go through each node, randomly decide to crash based on NodeCrashPercentage
	for i, node := range n.nodes {
		isCrashed := node.isCrashed.Load()
		if isCrashed {
			// randomly decide to recover based on NodeRecoverPercentage
			if n.randomness.Float64() < n.config.NodeRecoverProbability {
				n.startNode(i)
				recoveredNodes = append(recoveredNodes, node.E.ID)
				maxLeftToCrash++
			}
			continue
		}

		// check if we can still crash more nodes
		if maxLeftToCrash <= 0 {
			continue
		}

		// randomly decide to crash the node
		if n.randomness.Float64() < n.config.NodeCrashProbability {
			maxLeftToCrash--
			n.crashNode(i)
			crashedNodes = append(crashedNodes, node.E.ID)
		}
	}

	if len(recoveredNodes)+len(crashedNodes) > 0 {
		n.logger.Info("Recovered and crashed nodes", zap.Stringers("crashed", crashedNodes), zap.Stringers("recovered", recoveredNodes), zap.Uint64("num crashed", n.numCrashedNodes()))
	}
}

func (n *Network) getMinHeight() uint64 {
	minHeight := n.nodes[0].storage.NumBlocks()
	for _, node := range n.nodes[1:] {
		height := node.storage.NumBlocks()

		if height < minHeight {
			minHeight = height
		}
	}
	return minHeight
}

func (n *Network) getMaxHeight() uint64 {
	maxHeight := n.nodes[0].storage.NumBlocks()
	for _, node := range n.nodes[1:] {
		height := node.storage.NumBlocks()

		if height > maxHeight {
			maxHeight = height
		}
	}
	return maxHeight
}

func (n *Network) SetInfoLog() {
	n.lock.Lock()
	defer n.lock.Unlock()

	for _, node := range n.nodes {
		node.logger.SetLevel(zapcore.InfoLevel)
	}
}

func (n *Network) PrintStatus() {
	n.lock.Lock()
	defer n.lock.Unlock()

	// prints the number of nodes
	n.logger.Info("Network Status", zap.Int("num nodes", len(n.nodes)), zap.Int64("Seed", n.config.RandomSeed))

	// prints the number of txs in each node's mempool
	for _, node := range n.nodes {
		n.logger.Info("Node Status", zap.Stringer("nodeID", node.E.ID), zap.Int("Short", int(node.E.ID[0])), zap.Uint64("Round", node.E.Metadata().Round), zap.Uint64("Height", node.storage.NumBlocks()))
		node.PrintMessageTypesSent()
	}

	// prints total issued txs and failed txs
	n.logger.Info("Transaction Stats", zap.Int("total issued txs", n.allIssuedTxs))
}

func (n *Network) crashNode(idx int) {
	n.logger.Debug("Crashing node", zap.Stringer("nodeID", n.nodes[idx].E.ID))
	n.nodes[idx].isCrashed.Store(true)
	n.nodes[idx].Stop()
}

func (n *Network) startNode(idx int) {
	instance := n.nodes[idx]
	nodeID := instance.E.ID
	mempool := instance.mempool
	clonedWal := instance.wal.Clone()
	clonedStorage := instance.storage.Clone()
	mempool.Clear()

	newNode := NewNode(n.t, nodeID, n.BasicInMemoryNetwork, n.config, randomNodeConfig{
		mempool: mempool,
		wal:     clonedWal,
		storage: clonedStorage,
		logger:  instance.logger,
	})
	n.nodes[idx] = newNode
	n.ReplaceNode(newNode.BasicNode)

	newNode.Start()
}
