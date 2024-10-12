// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"io"
)

type NodePod struct {
	node *Node
}

func (p *NodePod) readState() error {
	// TODO(marun) What to do here?
	return nil
}

// Start waits for the process context to be written which
// indicates that the node will be accepting connections on
// its staking port. The network will start faster with this
// synchronization due to the avoidance of exponential backoff
// if a node tries to connect to a beacon that is not ready.
func (p *NodePod) Start(w io.Writer) error {
	// Create a statefulset for the pod and wait for it to become readycccccbiefkvivrggdtcnjfebeuvlkgrreicrnfcjtutn

	// Start a node and wait for it to become ready (accept connections on its API port)
	return nil
}

// Signals the node process to stop.
func (p *NodePod) InitiateStop() error {
	// Set the replicas to zero on the statefulset
	return nil
}

// Waits for the node process to stop.
func (p *NodePod) WaitForStopped(_ context.Context) error {
	// Wait for the status on the replicaset to indicate no replicas
	return nil
}

func (p *NodePod) IsHealthy(_ context.Context) (bool, error) {
	return false, nil
}
