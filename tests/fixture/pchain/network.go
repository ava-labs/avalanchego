// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package pchain provides public-API actions, verifiers, and observations
// shared across integration, e2e, and Antithesis tests.
package pchain

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

// Node is the P-Chain client of one node. NodeID may be empty, as for a
// single VM.
type Node struct {
	NodeID ids.NodeID
	URI    string
	Client *platformvm.Client
}

// NewNode returns a Node whose client targets uri.
func NewNode(nodeID ids.NodeID, uri string) Node {
	return Node{
		NodeID: nodeID,
		URI:    uri,
		Client: platformvm.NewClient(uri),
	}
}

func (n Node) String() string {
	if n.NodeID == ids.EmptyNodeID {
		return n.URI
	}
	return fmt.Sprintf("%s (%s)", n.NodeID, n.URI)
}

// NodeTxStatus is one node's answer to a transaction status query.
type NodeTxStatus struct {
	Node   Node
	Status status.Status
	Reason string
	Err    error
}

func (o NodeTxStatus) String() string {
	switch {
	case o.Err != nil:
		return fmt.Sprintf("%s: %v", o.Node, o.Err)
	case o.Reason != "":
		return fmt.Sprintf("%s: %s (%s)", o.Node, o.Status, o.Reason)
	default:
		return fmt.Sprintf("%s: %s", o.Node, o.Status)
	}
}

// ObserveNodes calls observe on every node concurrently and returns the
// results in node order.
func ObserveNodes[T any](nodes []Node, observe func(Node) T) []T {
	results := make([]T, len(nodes))
	var wg sync.WaitGroup
	wg.Add(len(nodes))
	for i, node := range nodes {
		go func() {
			defer wg.Done()
			results[i] = observe(node)
		}()
	}
	wg.Wait()
	return results
}

// GetTxStatus queries one node for the transaction's status.
func GetTxStatus(ctx context.Context, node Node, txID ids.ID) NodeTxStatus {
	nodeStatus := NodeTxStatus{Node: node}
	result, err := node.Client.GetTxStatus(ctx, txID)
	if err != nil {
		nodeStatus.Err = err
	} else {
		nodeStatus.Status = result.Status
		nodeStatus.Reason = result.Reason
	}
	return nodeStatus
}

// WaitForTxCommitted fails unless every node reports the transaction committed
// in the same round before the test context's default timeout. It fails
// immediately if any node reports the transaction aborted or every node
// reports it dropped.
func WaitForTxCommitted(tc tests.TestContext, nodes []Node, txID ids.ID) {
	const pollInterval = 500 * time.Millisecond

	require.NotEmpty(tc, nodes)

	ctx := tc.ContextWithTimeout(tests.DefaultTimeout)
	for {
		last := ObserveNodes(nodes, func(node Node) NodeTxStatus {
			return GetTxStatus(ctx, node, txID)
		})
		committed, dropped := true, true
		for _, nodeStatus := range last {
			require.NotEqual(tc, status.Aborted, nodeStatus.Status, "transaction %s aborted: %s", txID, nodeStatus)
			committed = committed && nodeStatus.Err == nil && nodeStatus.Status == status.Committed
			dropped = dropped && nodeStatus.Err == nil && nodeStatus.Status == status.Dropped
		}
		if committed {
			return
		}
		require.False(tc, dropped, "transaction %s dropped on every node: %v", txID, last)

		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			require.NoError(tc, ctx.Err(), "transaction %s did not commit on every node; last statuses: %v", txID, last)
			return
		case <-timer.C:
		}
	}
}
